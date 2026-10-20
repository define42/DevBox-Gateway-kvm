GOLANGCI_LINT_VERSION = v2.12.2

GO ?= $(shell go env GOROOT)/bin/go
QCOW2_IMAGE ?= ./rocky9-xfce-v2.4.16.qcow2
QCOW2_DISK ?= $(basename $(QCOW2_IMAGE)).gokvm.qcow2
QCOW2_VARS ?= $(QCOW2_DISK).vars.fd
QCOW2_SEED ?= $(QCOW2_DISK).seed.iso
QCOW2_USER ?= rocky
QCOW2_KEYBOARD ?= us
QCOW2_ARGS ?=
GATEWAY_STATE ?= ./.gokvm-gateway
GATEWAY_IMAGE ?=
GATEWAY_ARGS ?=
LDAP_PORT ?= 1636
GATEWAY_LDAP_LOCAL ?= 1
GATEWAY_LDAP_URL ?= ldaps://127.0.0.1:$(LDAP_PORT)
GATEWAY_LDAP_BASE_DN ?= dc=glauth,dc=com
GATEWAY_LDAP_DOMAIN ?= example.com
GATEWAY_LDAP_FILTER ?= (mail=%s)
GATEWAY_LDAP_USERNAME_ATTRIBUTE ?= mail
GATEWAY_LDAP_REQUIRED_GROUP ?= ou=team10_r,ou=groups,dc=glauth,dc=com
GATEWAY_LDAP_ADMIN_GROUP ?= devbox-admins
GATEWAY_LDAP_CA ?= ./testldap/.tls/cert.pem
OVMF_CODE ?= /usr/share/OVMF/OVMF_CODE_4M.fd
OVMF_VARS ?= /usr/share/OVMF/OVMF_VARS_4M.fd

# Docker builds use their own Go toolchain.
ifneq ($(MAKECMDGOALS),gateway-docker)
export GOPATH := $(shell $(GO) env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)
endif

$(GOPATH)/bin/stringer:
	$(GO) install golang.org/x/tools/cmd/stringer@latest

# Let Go track nested package sources and embedded assets through its build cache.
.PHONY: gokvm
gokvm:
	$(MAKE) generate
	$(GO) build -o gokvm .

golangci-lint:
	curl --retry 5 -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh \
		| sh -s -- -b . $(GOLANGCI_LINT_VERSION)

.PHONY: generate
generate: $(GOPATH)/bin/stringer
	$(GO) generate ./...

.PHONY: golangci
golangci: golangci-lint
	$(MAKE) generate
	./golangci-lint run ./...
	./golangci-lint run --build-tags=integration ./internal/vmm ./internal/machine ./internal/virtio

.PHONY: test
test:
	$(MAKE) generate
	$(MAKE) golangci
	$(MAKE) coverage
	$(MAKE) test-qcow2
	$(MAKE) test-audio
	$(GO) mod tidy -diff

# Count statements reached through other packages as well as package-local tests.
# Keep all module packages in the denominator, including bundled codec tools.
.PHONY: coverage
coverage:
	# A fresh network namespace starts with loopback down.
	unshare --user --net --map-root-user \
		sh -c 'ip link set lo up && exec "$$@"' sh \
		"$(GO)" test -count=1 -timeout 30m -covermode=atomic -coverpkg=./... -coverprofile c.out ./...
	$(MAKE) coverage-check

.PHONY: coverage-check
coverage-check:
	awk -v minimum=80 -f scripts/check-coverage.awk c.out

# Exercise the gateway's disk/firmware path without external kernel fixtures.
.PHONY: test-qcow2
test-qcow2:
	GOKVM_REQUIRE_UEFI_TEST=1 \
		GOKVM_TEST_OVMF_CODE="$(OVMF_CODE)" \
		GOKVM_TEST_OVMF_VARS="$(OVMF_VARS)" \
		"$(GO)" test -tags=integration -count=1 -timeout 3m \
		-run '^TestUEFIQCOW2Boot$$' ./internal/vmm -v

# Check UEFI sound registration and negotiated RDP playback without a guest image.
.PHONY: test-audio
test-audio:
	"$(GO)" test -tags=integration -count=1 -timeout 3m \
		-run '^Test(UEFISound|RDPDisplayAudio)' ./internal/machine ./internal/virtio -v

.PHONY: clean
clean:
	rm -f ./gokvm ./golangci-lint *_string.go

.PHONY: qcow2
qcow2: generate
	@test -r "$(OVMF_CODE)" || { echo "UEFI firmware not found; set OVMF_CODE to a plain OVMF CODE image." >&2; exit 1; }
	@set -eu; if [ ! -e "$(QCOW2_DISK)" ]; then \
		command -v qemu-img >/dev/null 2>&1 || { echo "Install qemu-utils for qemu-img to prepare the disk." >&2; exit 1; }; \
		test -r "$(QCOW2_IMAGE)" || { echo "Source disk not found: $(QCOW2_IMAGE)" >&2; exit 1; }; \
		echo "Preparing uncompressed working disk: $(QCOW2_DISK)"; \
		tmp=$$(mktemp "$(QCOW2_DISK).tmp.XXXXXX"); \
		trap 'rm -f -- "$$tmp"' EXIT; \
		qemu-img convert -f qcow2 -O qcow2 "$(QCOW2_IMAGE)" "$$tmp"; \
		ln -- "$$tmp" "$(QCOW2_DISK)"; \
	fi
	@set -eu; if [ ! -e "$(QCOW2_VARS)" ]; then \
		test -r "$(OVMF_VARS)" || { echo "UEFI variables template not found; set OVMF_VARS to the matching template." >&2; exit 1; }; \
		echo "Creating persistent UEFI variables: $(QCOW2_VARS)"; \
		tmp=$$(mktemp "$(QCOW2_VARS).tmp.XXXXXX"); \
		trap 'rm -f -- "$$tmp"' EXIT; \
		cat -- "$(OVMF_VARS)" > "$$tmp"; \
		ln -- "$$tmp" "$(QCOW2_VARS)"; \
	fi
	CGO_ENABLED=0 $(GO) build -o gokvm .
	@set -eu; if [ ! -e "$(QCOW2_SEED)" ]; then \
		./gokvm seed -out "$(QCOW2_SEED)" -user "$(QCOW2_USER)" -keyboard "$(QCOW2_KEYBOARD)"; \
	fi
	@echo "Guest login: $(QCOW2_SEED).credentials.json (generated on first boot preparation)."
	@echo "Headless developer boot. Use make gateway and the dashboard for RDP access."
	./gokvm boot -cloud-init "$(QCOW2_SEED)" -uefi "$(OVMF_CODE)" -uefi-vars "$(QCOW2_VARS)" -d "$(QCOW2_DISK)" \
		-c 2 -m 4G -net user $(QCOW2_ARGS)

.PHONY: freerdp
freerdp:
	./tools/build-freerdp.sh

# Local native Gateway: HTTPS UI/auth and token-routed RDP share port 8443.
.PHONY: ldap ldap-stop gateway-ldap-test
ldap:
	./testldap/generate-cert.sh
	LDAP_PORT="$(LDAP_PORT)" docker compose -f testldap/compose.yaml up -d ldap
	./testldap/wait-ready.sh "$(LDAP_PORT)"

ldap-stop:
	LDAP_PORT="$(LDAP_PORT)" docker compose -f testldap/compose.yaml stop ldap

gateway-ldap-test: ldap
	GOKVM_TEST_LDAP_URL="ldaps://127.0.0.1:$(LDAP_PORT)" \
		GOKVM_TEST_LDAP_CA="$(abspath testldap/.tls/cert.pem)" \
		$(GO) test -race ./internal/gateway -run '^TestBundledLDAP' -count=1

# Rebuild and recreate the container after source or configuration changes.
.PHONY: gateway-docker
gateway-docker:
	./docker/configure.sh
	docker compose up --build -d gateway

# Native gateway configuration uses the same ENV keys as the executable.
# Scope native defaults to this target so Docker uses Compose/.env settings.
# Retain the GATEWAY_* make variables as defaults for existing invocations.
gateway: export DATA_ROOT_DIR ?= $(GATEWAY_STATE)
gateway: export BASE_IMAGE ?= $(GATEWAY_IMAGE)
gateway: export UEFI_CODE ?= $(OVMF_CODE)
gateway: export UEFI_VARS_TEMPLATE ?= $(OVMF_VARS)
gateway: export LDAP_URL ?= $(GATEWAY_LDAP_URL)
gateway: export LDAP_BASE_DN ?= $(GATEWAY_LDAP_BASE_DN)
gateway: export LDAP_USER_DOMAIN ?= $(GATEWAY_LDAP_DOMAIN)
gateway: export LDAP_USER_FILTER ?= $(GATEWAY_LDAP_FILTER)
gateway: export LDAP_USERNAME_ATTRIBUTE ?= $(GATEWAY_LDAP_USERNAME_ATTRIBUTE)
gateway: export LDAP_REQUIRED_GROUP ?= $(GATEWAY_LDAP_REQUIRED_GROUP)
gateway: export ADMIN_GROUP ?= $(GATEWAY_LDAP_ADMIN_GROUP)
gateway: export LDAP_CA_FILE ?= $(GATEWAY_LDAP_CA)

.PHONY: gateway
ifeq ($(GATEWAY_LDAP_LOCAL),1)
gateway: ldap
endif
gateway: generate
	CGO_ENABLED=1 $(GO) build -tags nvenc -o gokvm .
	@set -eu; mkdir -p -m 700 "$(DATA_ROOT_DIR)"; \
	if [ -n "$(BASE_IMAGE)" ] && [ ! -e "$(BASE_IMAGE)" ]; then \
		command -v qemu-img >/dev/null 2>&1 || { echo "Install qemu-utils for one-time image conversion." >&2; exit 1; }; \
		test -r "$(QCOW2_IMAGE)" || { echo "Source disk not found: $(QCOW2_IMAGE)" >&2; exit 1; }; \
		tmp=$$(mktemp "$(BASE_IMAGE).tmp.XXXXXX"); \
		trap 'rm -f -- "$$tmp"' EXIT; \
		qemu-img convert -f qcow2 -O qcow2 "$(QCOW2_IMAGE)" "$$tmp"; \
		ln -- "$$tmp" "$(BASE_IMAGE)"; \
	fi
	@echo "Open https://localhost:8443 and sign in with your LDAP account. See testldap/README.md for local accounts."
	./gokvm serve $(GATEWAY_ARGS)
