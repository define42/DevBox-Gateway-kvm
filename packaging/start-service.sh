#!/bin/sh
# systemd runs this launcher as the unprivileged gateway user, in its supervisor
# subgroup. All filesystem writes below stay within this service's delegation.
set -eu
umask 077

fail() {
    printf 'devbox-kvm: %s\n' "$*" >&2
    exit 1
}

if [ "$(id -u)" = 0 ] || [ "$(id -un)" != devbox-kvm ]; then
    fail 'start the gateway with systemctl start devbox-kvm.service.'
fi
if [ -z "${LDAP_URL:-}" ] && [ -z "${AUTH_FILE:-}" ]; then
    fail 'configure LDAP_URL and LDAP_BASE_DN in /etc/devbox-kvm/devbox-kvm.env first.'
fi

service=/sys/fs/cgroup/system.slice/devbox-kvm.service
workers="$service/workers"
expected=/system.slice/devbox-kvm.service/supervisor
actual=
while IFS=: read -r hierarchy controllers path; do
    if [ "$hierarchy" = 0 ] && [ -z "$controllers" ]; then
        actual=$path
    fi
done < /proc/self/cgroup
if [ "$actual" != "$expected" ]; then
    fail 'the packaged service requires systemd 254+ and DelegateSubgroup=supervisor.'
fi
if [ "$(stat -f -c %T "$service")" != cgroup2fs ]; then
    fail 'the unified cgroup v2 hierarchy is required.'
fi
if [ -L "$workers" ]; then
    fail 'the VM cgroup must not be a symbolic link.'
fi
if [ -d "$workers" ] && ! grep -qx 'populated 0' "$workers/cgroup.events"; then
    fail 'the VM subtree still contains processes; stop the service before retrying.'
fi

printf '+memory +pids\n' > "$service/cgroup.subtree_control"
mkdir -p -- "$workers"
chmod 0700 "$workers"
printf '+memory +pids\n' > "$workers/cgroup.subtree_control"
if [ ! -w "$workers/cgroup.kill" ]; then
    fail 'writable cgroup.kill is required (Linux 5.14+).'
fi
for control in cgroup.procs cgroup.threads cgroup.subtree_control; do
    chmod 0600 "$workers/$control"
done

# The service owns this exact subtree; do not accept an unrelated path from the
# environment file. Worker startup validates ownership, controllers and emptiness.
VM_CGROUP_ROOT=$workers
export VM_CGROUP_ROOT
exec /usr/bin/devbox-kvm serve
