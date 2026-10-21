#!/bin/sh
# Install persistent VM delegation for the supplied rootful Docker deployment.
set -eu
cd -- "$(dirname -- "$0")/.."

if [ "$(id -u)" != 0 ]; then
    echo 'Run sudo ./docker/setup-cgroup.sh to install the host delegation.' >&2
    exit 1
fi
if [ ! -d /run/systemd/system ] || [ "$(stat -f -c %T /sys/fs/cgroup)" != cgroup2fs ]; then
    echo 'This helper requires systemd and cgroup v2.' >&2
    exit 1
fi
systemd_version=$(systemctl --version | awk 'NR == 1 {print $2}')
if [ "$systemd_version" -lt 254 ]; then
    echo 'This helper requires systemd 254 or newer (DelegateSubgroup).' >&2
    exit 1
fi
if [ "$(docker info --format '{{.CgroupDriver}}')" != systemd ]; then
    echo 'This helper requires Docker with the systemd cgroup driver.' >&2
    exit 1
fi
security=$(docker info --format '{{json .SecurityOptions}}')
case "$security" in
    *rootless*|*userns*)
        echo 'This helper requires rootful Docker without UID remapping.' >&2
        exit 1
        ;;
esac
install -d -m 0755 /usr/local/libexec /etc/systemd/system/docker.service.d
install -m 0755 docker/prepare-cgroup.sh /usr/local/libexec/devbox-prepare-cgroup
install -m 0644 docker/systemd/devbox.slice /etc/systemd/system/devbox.slice
install -m 0644 docker/systemd/devbox-vm-cgroup.service /etc/systemd/system/devbox-vm-cgroup.service
install -m 0644 docker/systemd/devbox-cgroup.conf /etc/systemd/system/docker.service.d/devbox-cgroup.conf
systemctl daemon-reload
# Starting an already-active service does not restart its existing workers.
systemctl enable --now devbox-vm-cgroup.service
workers=/sys/fs/cgroup/devbox.slice/devbox-vm-cgroup.service/workers
if [ ! -d "$workers" ] || [ "$(stat -c '%u:%a' "$workers")" != '10001:700' ] ||
    ! grep -qw memory "$workers/cgroup.subtree_control" ||
    ! grep -qw pids "$workers/cgroup.subtree_control"; then
    echo 'Memory/pids delegation is not ready; inspect systemctl status devbox-vm-cgroup.service.' >&2
    exit 1
fi
printf 'Host delegation ready. Include compose.cgroup.yaml when starting the gateway.\n'
