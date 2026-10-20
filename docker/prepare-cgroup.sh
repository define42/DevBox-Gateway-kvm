#!/bin/sh
# Run only by devbox-vm-cgroup.service, in its control subgroup.
set -eu
umask 077

slice=/sys/fs/cgroup/devbox.slice
service="$slice/devbox-vm-cgroup.service"
workers="$service/workers"
expected=/devbox.slice/devbox-vm-cgroup.service/.control
actual=$(awk -F: '$1 == "0" {print $3}' /proc/self/cgroup)
if [ "$(id -u)" != 0 ] || [ "$actual" != "$expected" ]; then
    echo 'Run this helper through devbox-vm-cgroup.service.' >&2
    exit 1
fi
if [ "$(stat -f -c %T "$service")" != cgroup2fs ]; then
    echo 'DevBox requires the unified cgroup v2 hierarchy.' >&2
    exit 1
fi
if [ -d "$workers" ] && ! grep -qx 'populated 0' "$workers/cgroup.events"; then
    echo 'The VM subtree still contains processes; stop the gateway before preparing it.' >&2
    exit 1
fi
printf '+memory\n' > "$service/cgroup.subtree_control"
mkdir -p -- "$workers"
printf '+memory\n' > "$workers/cgroup.subtree_control"
chown 10001:10001 "$workers"
chmod 0700 "$workers"
if [ ! -e "$workers/cgroup.kill" ]; then
    echo 'DevBox requires cgroup.kill (Linux 5.14 or newer).' >&2
    exit 1
fi
# Grant only delegation controls, never ownership of the host hierarchy.
for control in cgroup.procs cgroup.threads cgroup.subtree_control; do
    chown 10001:10001 "$workers/$control"
    chmod 0600 "$workers/$control"
done
# clone3 checks write access at the common ancestor of the Docker scope and
# workers. This one control stays outside the container's writable bind mount.
chown 10001:10001 "$slice/cgroup.procs"
chmod 0600 "$slice/cgroup.procs"
printf 'Delegated %s to UID 10001.\n' "$workers"
