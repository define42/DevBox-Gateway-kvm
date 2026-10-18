#!/bin/bash -x
# ----------------------------------------------------------------------
# get_kernel.bash - download or build bzImage and vmlinux
# ----------------------------------------------------------------------

set -euo pipefail

bzImage=${1:-bzImage}
vmlinux=${2:-vmlinux}
config=${3:-linux.config}

# Download pre-compiled bzImage
curl -fsSL --retry 5 -o "${bzImage}" \
  "https://github.com/bobuhiro11/bins/raw/main/gokvm/${bzImage}"

# Download pre-compiled EDK2/CloudHv firmware image (CLOUDHV.fd)
curl -fsSL --retry 5 -o CLOUDHV.fd \
  https://github.com/bobuhiro11/bins/raw/main/gokvm/CLOUDHV.fd

# Download extract-ikconfig script
curl -fsSL --retry 5 -o extract-ikconfig \
  https://raw.githubusercontent.com/torvalds/linux/master/scripts/extract-ikconfig
chmod a+x ./extract-ikconfig

# Download extract-vmlinux script
curl -fsSL --retry 5 -o extract-vmlinux \
  https://raw.githubusercontent.com/torvalds/linux/master/scripts/extract-vmlinux
chmod a+x ./extract-vmlinux

# Keep extraction outside the conditional so failures stop the script.
downloaded_config=$(./extract-ikconfig "${bzImage}")

# A configuration mismatch is expected when the prebuilt kernel is outdated.
if ! diff -u <(printf '%s\n' "${downloaded_config}") "${config}"; then
  version=$(awk '/Kernel Configuration/ {print $3}' "${config}")
  # Initial releases use linux-X.Y.tar.xz, although Kconfig records X.Y.0.
  version=${version%.0}
  major_version=${version%%.*}
  archive="linux-${version}.tar.xz"
  build_dir="_linux/linux-${version}"
  curl -fsSL -C - --retry 5 -o "${archive}" \
    "https://cdn.kernel.org/pub/linux/kernel/v${major_version}.x/${archive}"

  # Do not mix source files or build products from different kernel versions.
  mkdir -p "${build_dir}"
  tar -Jxf "${archive}" -C "${build_dir}" --strip-components 1
  cp "${config}" "${build_dir}/.config"
  make -C "${build_dir}" olddefconfig
  make -j"$(nproc)" -C "${build_dir}"
  cp "${build_dir}/arch/x86/boot/bzImage" "${bzImage}"
fi

# Extract vmlinux from bzImage
./extract-vmlinux "${bzImage}" > "${vmlinux}"
