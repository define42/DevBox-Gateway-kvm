#!/usr/bin/env bash
# Build the pinned OpenH264 release with its NASM assembly and the patches in
# tools/openh264-patches into a static library for the cgo software encoder
# (internal/rdp/avc/internal/openh264).
set -euo pipefail

openh264_tools=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
openh264_root=$(cd -- "$openh264_tools/.." && pwd)
openh264_version=v2.7.0
# The tag's commit pins the exact sources; a shallow clone is verified against it.
openh264_commit=1bca62311e447fd9c39fc93de2c4b96cd5ce971c
openh264_cache="$openh264_tools/.cache"
openh264_source="$openh264_cache/openh264-$openh264_version"
openh264_install="$openh264_root/third_party/openh264"
openh264_patches=("$openh264_tools"/openh264-patches/*.patch)
# A patch edit changes the stamp and forces a clean rebuild.
openh264_patch_hash=$(cat -- "${openh264_patches[@]}" | sha256sum | cut -c1-16)
openh264_stamp="$openh264_install/.build-$openh264_commit-$openh264_patch_hash"

if [[ -f "$openh264_stamp" && -f "$openh264_install/lib/libopenh264.a" ]]; then
    exit 0
fi

for openh264_command in git make c++ cc nasm sha256sum; do
    if ! command -v "$openh264_command" >/dev/null 2>&1; then
        echo "Missing $openh264_command; OpenH264 needs git, make, a C/C++ compiler and NASM (apt install nasm)." >&2
        exit 1
    fi
done

mkdir -p "$openh264_cache"
if [[ ! -d "$openh264_source/.git" ]]; then
    rm -rf -- "$openh264_source.part"
    git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$openh264_version" \
        https://github.com/cisco/openh264 "$openh264_source.part"
    mv -- "$openh264_source.part" "$openh264_source"
fi
if [[ "$(git -C "$openh264_source" rev-parse HEAD)" != "$openh264_commit" ]]; then
    echo "OpenH264 $openh264_version does not match commit $openh264_commit." >&2
    exit 1
fi
# Start from pristine sources so stale patches and objects cannot leak in.
git -C "$openh264_source" reset --quiet --hard "$openh264_commit"
git -C "$openh264_source" clean --quiet -fdx
for openh264_patch in "${openh264_patches[@]}"; do
    git -C "$openh264_source" apply --whitespace=nowarn "$openh264_patch"
done

# USE_ASM=Yes assembles the x86 SSE2/SSSE3/AVX2 kernels with NASM; the library
# selects them at run time from CPUID. Objects are position independent.
make -C "$openh264_source" -j"$(nproc)" BUILDTYPE=Release USE_ASM=Yes libopenh264.a >/dev/null

rm -rf -- "$openh264_install"
mkdir -p "$openh264_install/lib" "$openh264_install/include/wels"
cp -- "$openh264_source/libopenh264.a" "$openh264_install/lib/"
cp -- "$openh264_source"/codec/api/wels/codec_*.h "$openh264_install/include/wels/"
cp -- "$openh264_source/LICENSE" "$openh264_install/LICENSE"
touch -- "$openh264_stamp"
echo "OpenH264 $openh264_version installed in $openh264_install"
