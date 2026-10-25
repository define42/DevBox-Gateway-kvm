#!/bin/sh
# systemd runs this launcher as the unprivileged gateway user, in its supervisor
# subgroup. All filesystem writes below stay within this service's delegation.
set -eu
umask 077

fail() {
    printf 'devbox-kvm: %s\n' "$*" >&2
    # The unit suppresses retries for prerequisites that need administrator action.
    exit 78
}

uid=$(id -u) || fail 'cannot determine the service UID; check the devbox-kvm account.'
user=$(id -un) || fail 'cannot resolve the service user; check the devbox-kvm account.'
if [ "$uid" = 0 ] || [ "$user" != devbox-kvm ]; then
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
done < /proc/self/cgroup || fail 'cannot read the service cgroup membership from /proc/self/cgroup.'
if [ "$actual" != "$expected" ]; then
    fail 'the packaged service requires systemd 254+ and DelegateSubgroup=supervisor.'
fi
filesystem=$(stat -f -c %T "$service") ||
    fail 'cannot inspect the service cgroup; check the systemd cgroup v2 delegation.'
if [ "$filesystem" != cgroup2fs ]; then
    fail 'the unified cgroup v2 hierarchy is required.'
fi
if [ -L "$workers" ]; then
    fail 'the VM cgroup must not be a symbolic link.'
fi
if [ -d "$workers" ] && ! grep -qx 'populated 0' "$workers/cgroup.events"; then
    fail 'cannot confirm the VM subtree is empty; stop the service and check workers/cgroup.events before retrying.'
fi

printf '+memory +pids\n' > "$service/cgroup.subtree_control" ||
    fail 'cannot enable memory and pids controllers; check Delegate=memory pids and the service cgroup permissions.'
mkdir -p -- "$workers" ||
    fail 'cannot create the VM cgroup; check the service cgroup delegation and permissions.'
chmod 0700 "$workers" ||
    fail 'cannot restrict access to the VM cgroup; check its ownership and permissions.'
printf '+memory +pids\n' > "$workers/cgroup.subtree_control" ||
    fail 'cannot delegate memory and pids to VM workers; check the workers cgroup controllers and permissions.'
if [ ! -w "$workers/cgroup.kill" ]; then
    fail 'writable cgroup.kill is required (Linux 5.14+).'
fi
for control in cgroup.procs cgroup.threads cgroup.subtree_control; do
    chmod 0600 "$workers/$control" ||
        fail "cannot restrict access to VM cgroup control $control; check its ownership and permissions."
done

# The service owns this exact subtree; do not accept an unrelated path from the
# environment file. Worker startup validates ownership, controllers and emptiness.
VM_CGROUP_ROOT=$workers
export VM_CGROUP_ROOT
exec /usr/bin/devbox-kvm serve
