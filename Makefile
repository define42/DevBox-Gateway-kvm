GOLANGCI_LINT_VERSION = v2.12.2

GO ?= $(shell go env GOROOT)/bin/go
NETDESK_UKI ?= ../NetDesk/dist/netdesk.efi
NETDESK_ARGS ?=
QCOW2_IMAGE ?= ./rocky9-xfce-v2.4.16.qcow2
QCOW2_DISK ?= $(basename $(QCOW2_IMAGE)).gokvm.qcow2
QCOW2_VARS ?= $(QCOW2_DISK).vars.fd
QCOW2_ARGS ?=
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

gokvm: $(wildcard *.go) $(wildcard */*.go)
	$(MAKE) generate
	$(GO) build .

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

.PHONY: slax
slax: generate
	CGO_ENABLED=0 $(GO) build -o gokvm .
	./gokvm boot -iso ./slax.iso -c 4 -m 2G -net user -rdp 127.0.0.1:3390 \
		-rdp-h264 -rdp-h264-threads 4

.PHONY: netdesk
netdesk: generate
	CGO_ENABLED=1 $(GO) build -tags nvenc -o gokvm .
	./gokvm boot -uki "$(NETDESK_UKI)" -c 2 -m 6G -net user -rdp 127.0.0.1:3390 \
		-rdp-h264 -rdp-h264-encoder nvenc $(NETDESK_ARGS)

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
	./gokvm boot -uefi "$(OVMF_CODE)" -uefi-vars "$(QCOW2_VARS)" -d "$(QCOW2_DISK)" \
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
