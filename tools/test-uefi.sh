#!/usr/bin/env bash
# Native OVMF disk boot, persistent variables, reset and shutdown.
set -euo pipefail

uefi_tools_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
uefi_repo_dir=$(cd -- "$uefi_tools_dir/.." && pwd)
: "${OVMF_CODE:?Set OVMF_CODE to a plain non-SMM OVMF_CODE firmware file}"
: "${OVMF_VARS:?Set OVMF_VARS to its matching OVMF_VARS template}"

for uefi_command in cc ld objcopy mformat mmd mcopy qemu-img timeout truncate python3; do
    if ! command -v "$uefi_command" >/dev/null 2>&1; then
        echo "Missing $uefi_command; see the UEFI smoke-test dependencies in README.md." >&2
        exit 1
    fi
done
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
    echo 'The UEFI test requires read/write access to /dev/kvm.' >&2
    exit 1
fi
if [[ ! -f "$OVMF_CODE" || ! -f "$OVMF_VARS" ]]; then
    echo 'OVMF_CODE and OVMF_VARS must name existing firmware files.' >&2
    exit 1
fi
uefi_include_dir=${EFI_INCLUDE_DIR:-/usr/include/efi}
uefi_lib_dir=${EFI_LIB_DIR:-}
if [[ -z "$uefi_lib_dir" ]]; then
    for uefi_candidate in /usr/lib /usr/lib64 /usr/lib/x86_64-linux-gnu /usr/lib/gnuefi /usr/lib64/gnuefi; do
        if [[ -f "$uefi_candidate/crt0-efi-x86_64.o" ]]; then
            uefi_lib_dir=$uefi_candidate
            break
        fi
    done
fi
for uefi_file in crt0-efi-x86_64.o elf_x86_64_efi.lds libefi.a libgnuefi.a; do
    if [[ ! -f "$uefi_lib_dir/$uefi_file" ]]; then
        echo "Missing GNU-EFI $uefi_file; install gnu-efi or set EFI_LIB_DIR." >&2
        exit 1
    fi
done
if [[ ! -f "$uefi_include_dir/efi.h" ]]; then
    echo 'Missing GNU-EFI headers; install gnu-efi or set EFI_INCLUDE_DIR.' >&2
    exit 1
fi

uefi_test_dir=$(mktemp -d "${TMPDIR:-/tmp}/devbox-kvm-uefi-smoke.XXXXXX")
uefi_cleanup() {
    local uefi_status=$?
    if [[ $uefi_status == 0 && ${UEFI_KEEP_ARTIFACTS:-0} != 1 ]]; then
        rm -rf -- "$uefi_test_dir"
    else
        echo "UEFI test artifacts: $uefi_test_dir" >&2
    fi
}
trap uefi_cleanup EXIT

uefi_binary=${DEVBOX_KVM_BINARY:-}
if [[ -z "$uefi_binary" ]]; then
    uefi_binary="$uefi_test_dir/devbox-kvm"
    (cd -- "$uefi_repo_dir" && CGO_ENABLED=0 go build -o "$uefi_binary" .)
fi
cc -I"$uefi_include_dir" -I"$uefi_include_dir/x86_64" -I"$uefi_include_dir/protocol" \
    -fpic -ffreestanding -fno-stack-protector -fshort-wchar -mno-red-zone \
    -DEFI_FUNCTION_WRAPPER -Wall -Wextra -Werror \
    -c "$uefi_tools_dir/uefi-smoke.c" -o "$uefi_test_dir/smoke.o"
ld -nostdlib -znocombreloc -T "$uefi_lib_dir/elf_x86_64_efi.lds" -shared -Bsymbolic \
    "$uefi_lib_dir/crt0-efi-x86_64.o" "$uefi_test_dir/smoke.o" \
    -L"$uefi_lib_dir" -lefi -lgnuefi -o "$uefi_test_dir/smoke.so"
objcopy -j .text -j .sdata -j .data -j .dynamic -j .dynsym \
    -j .rel -j .rela -j .reloc --target=efi-app-x86_64 \
    "$uefi_test_dir/smoke.so" "$uefi_test_dir/BOOTX64.EFI"

truncate -s 64M "$uefi_test_dir/disk.raw"
mformat -i "$uefi_test_dir/disk.raw" -F ::
mmd -i "$uefi_test_dir/disk.raw" ::/EFI ::/EFI/BOOT
mcopy -i "$uefi_test_dir/disk.raw" "$uefi_test_dir/BOOTX64.EFI" ::/EFI/BOOT/BOOTX64.EFI
qemu-img convert -f raw -O qcow2 "$uefi_test_dir/disk.raw" "$uefi_test_dir/disk.qcow2"

cp -- "$OVMF_VARS" "$uefi_test_dir/qcow2.vars.fd"
uefi_log="$uefi_test_dir/qcow2.log"
echo "Testing UEFI QCOW2 boot, persistent reset and shutdown..."
if ! timeout --signal=TERM --kill-after=5s "${UEFI_TIMEOUT:-90s}" \
    "$uefi_binary" boot -uefi "$OVMF_CODE" \
    -uefi-vars "$uefi_test_dir/qcow2.vars.fd" \
    -d "$uefi_test_dir/disk.qcow2" -c "${UEFI_CPUS:-2}" -m 256M \
    </dev/null >"$uefi_log" 2>&1; then
    cat -- "$uefi_log" >&2
    echo "UEFI QCOW2 test did not shut down successfully." >&2
    exit 1
fi
python3 - "$uefi_log" <<'PY'
from pathlib import Path
import sys

log = Path(sys.argv[1]).read_text(errors="replace")
expected = {
    "DEVBOX_KVM TEST EFI ENTRY": 2,
    "DEVBOX_KVM TEST VARS WRITE OK": 2,
    "DEVBOX_KVM TEST RESET": 1,
    "DEVBOX_KVM TEST VARS PERSISTED": 1,
    "DEVBOX_KVM TEST SHUTDOWN": 1,
}
if "DEVBOX_KVM TEST FAILED" in log:
    raise SystemExit(log)
for marker, count in expected.items():
    actual = log.count(marker)
    if actual != count:
        raise SystemExit(f"{marker}: found {actual}, expected {count}\n{log}")
print("PASS: firmware disk boot, saved variables, reboot, shutdown")
PY
