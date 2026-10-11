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
GATEWAY_IMAGE ?= $(GATEWAY_STATE)/base.qcow2
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
RDP_CLIENT ?= $(shell if [ -x ./tools/freerdp/bin/xfreerdp ]; then \
	printf '%s\n' ./tools/freerdp/bin/xfreerdp; \
	else command -v xfreerdp3 2>/dev/null || command -v xfreerdp 2>/dev/null; fi)

export GOPATH := $(shell $(GO) env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)

$(GOPATH)/bin/stringer:
	$(GO) install golang.org/x/tools/cmd/stringer@latest

$(GOPATH)/src/github.com/u-root/u-root:
	git clone --depth=1 --branch v0.16.0 \
	  https://github.com/u-root/u-root.git $@

$(GOPATH)/bin/u-root: $(GOPATH)/src/github.com/u-root/u-root
	cd $(GOPATH)/src/github.com/u-root/u-root \
	  && $(GO) install .

# Let Go track nested package sources and embedded assets through its build cache.
.PHONY: gokvm
gokvm:
	$(MAKE) generate
	$(GO) build -o gokvm .

golangci-lint:
	curl --retry 5 -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh \
		| sh -s -- -b . $(GOLANGCI_LINT_VERSION)

vda.img:
	$(eval dir = $(shell mktemp -d))
	echo "index.html: this message is from /dev/vda in guest" > ${dir}/index.html
	genext2fs -b 1024 -d ${dir} $@
	file $@

# checkbinaries runs which on all the commands we want to include.
# Be sure to keep it up to date if you add new commands to the initrd
# rule below.
checkbinaries:
	@which ethtool
	@which lspci
	@which lsblk
	@which hexdump
	@which mount
	@which bash
	@which nohup
	@which clear
	@which tic
	@which awk
	@which grep
	@which cut

initrd: checkbinaries ./scripts/get_initrd.bash .bashrc \
  $(GOPATH)/bin/u-root \
  $(GOPATH)/src/github.com/u-root/u-root
	./scripts/get_initrd.bash

bzImage vmlinux: linux.config ./scripts/get_kernel.bash
	./scripts/get_kernel.bash

bzImage_PVH vmlinux_PVH CLOUDHV.fd: linux_pvh.config ./scripts/get_kernel.bash
	./scripts/get_kernel.bash \
		bzImage_PVH \
		vmlinux_PVH \
		linux_pvh.config

.PHONY: run
run: initrd bzImage
	$(MAKE) generate
	$(GO) run . boot -c 4 -i "./initrd"

.PHONY: runpvh
runpvh: initrd vmlinux_PVH
	$(MAKE) generate
	$(GO) run . boot -c 4 -k "./vmlinuz_PVH" -i "./initrd"

.PHONY: run-system-kernel
run-system-kernel:
	$(MAKE) generate
	# Implemented based on fedora's default path.
	# Other distributions need to be considered.
	$(GO) run . boot -k $(shell ls -t /boot/vmlinuz*.x86_64 | head -n 1) \
		-p "console=ttyS0 pci=off earlyprintk=serial nokaslr rdinit=/bin/sh" \
		-i $(shell ls -t /boot/initramfs*.x86_64.img | head -n 1)

.PHONY: generate
generate: $(GOPATH)/bin/stringer
	$(GO) generate ./...

.PHONY: golangci
golangci: golangci-lint
	$(MAKE) generate
	./golangci-lint run ./...

.PHONY: test
test: bzImage vmlinux vmlinux_PVH initrd vda.img CLOUDHV.fd
	$(MAKE) generate
	$(MAKE) golangci
	# A fresh network namespace starts with loopback down.
	unshare --user --net --map-root-user \
		sh -c 'ip link set lo up && exec "$$@"' sh \
		"$(GO)" test -timeout 30m -coverprofile c.out ./...
	$(GO) mod tidy -diff

.PHONY: clean
clean:
	rm -rf ./gokvm ./golangci-lint bzImage* vmlinux* CLOUDHV.fd _linux *_string.go

.PHONY: qemu
qemu: initrd bzImage
	qemu-system-x86_64 -kernel ./bzImage -initrd ./initrd --nographic --enable-kvm \
		--append "root=/dev/ram rw console=ttyS0 rdinit=/init" --enable-kvm
.PHONY: tinycore
tinycore: gokvm
	./gokvm boot -iso TinyCore-current.iso -vnc 127.0.0.1:5900 -m 512M

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
	CGO_ENABLED=1 $(GO) build -tags nvenc -o gokvm .
	@set -eu; if [ ! -e "$(QCOW2_SEED)" ]; then \
		./gokvm seed -out "$(QCOW2_SEED)" -user "$(QCOW2_USER)" -keyboard "$(QCOW2_KEYBOARD)"; \
	fi
	@echo "Guest login: $(QCOW2_SEED).credentials.json (generated on first boot preparation)."
	./gokvm boot -cloud-init "$(QCOW2_SEED)" -uefi "$(OVMF_CODE)" -uefi-vars "$(QCOW2_VARS)" -d "$(QCOW2_DISK)" \
		-c 2 -m 4G -net user -rdp 127.0.0.1:3390 -rdp-wait \
		-rdp-h264 -rdp-h264-encoder auto $(QCOW2_ARGS)

.PHONY: freerdp
freerdp:
	./tools/build-freerdp.sh

.PHONY: rdp
rdp:
	@client="$(RDP_CLIENT)"; \
	if ! command -v "$$client" >/dev/null 2>&1; then \
		echo "FreeRDP client not found; run make freerdp or set RDP_CLIENT=/path/to/xfreerdp." >&2; \
		exit 1; \
	fi; \
	if ! config=$$("$$client" /buildconfig 2>&1); then \
		echo "Cannot check FreeRDP H.264 support: $$config" >&2; \
		exit 1; \
	fi; \
	case "$$config" in \
		*WITH_GFX_H264=ON*|*WITH_GFX_H264=TRUE*) ;; \
		*) echo "FreeRDP H.264 support is required; run make freerdp or set RDP_CLIENT to an H.264-capable client." >&2; exit 1 ;; \
	esac; \
	exec "$$client" /v:127.0.0.1:3390 /sec:tls /u:console /p:console \
		/cert:ignore /dynamic-resolution /gfx:AVC420

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

.PHONY: gateway
ifeq ($(GATEWAY_LDAP_LOCAL),1)
gateway: ldap
endif
gateway: generate
	CGO_ENABLED=1 $(GO) build -tags nvenc -o gokvm .
	@set -eu; mkdir -p -m 700 "$(GATEWAY_STATE)"; \
	if [ ! -e "$(GATEWAY_IMAGE)" ]; then \
		command -v qemu-img >/dev/null 2>&1 || { echo "Install qemu-utils for one-time image conversion." >&2; exit 1; }; \
		tmp=$$(mktemp "$(GATEWAY_IMAGE).tmp.XXXXXX"); \
		trap 'rm -f -- "$$tmp"' EXIT; \
		qemu-img convert -f qcow2 -O qcow2 "$(QCOW2_IMAGE)" "$$tmp"; \
		ln -- "$$tmp" "$(GATEWAY_IMAGE)"; \
	fi
	@echo "Open https://localhost:8443 and sign in with your LDAP account. See testldap/README.md for local accounts."
	./gokvm serve -state "$(GATEWAY_STATE)" -image "$(GATEWAY_IMAGE)" \
		-uefi "$(OVMF_CODE)" -uefi-vars-template "$(OVMF_VARS)" \
		-ldap-url "$(GATEWAY_LDAP_URL)" -ldap-base-dn "$(GATEWAY_LDAP_BASE_DN)" \
		-ldap-domain "$(GATEWAY_LDAP_DOMAIN)" -ldap-filter '$(GATEWAY_LDAP_FILTER)' \
		-ldap-username-attribute "$(GATEWAY_LDAP_USERNAME_ATTRIBUTE)" \
		-ldap-required-group "$(GATEWAY_LDAP_REQUIRED_GROUP)" \
		-ldap-admin-group "$(GATEWAY_LDAP_ADMIN_GROUP)" -ldap-ca "$(GATEWAY_LDAP_CA)" $(GATEWAY_ARGS)
