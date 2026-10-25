#!/bin/sh
set -eu

# DEB calls postinst for more than successful configuration. RPM supplies the
# installed package count (1 for installation, 2 or more for an upgrade).
case "${1:-}" in
    configure|[0-9]*) ;;
    *) exit 0 ;;
esac

if ! getent group devbox-kvm >/dev/null; then
    groupadd --system devbox-kvm
fi
if ! getent group kvm >/dev/null; then
    groupadd --system kvm
fi
if ! getent passwd devbox-kvm >/dev/null; then
    useradd --system --gid devbox-kvm --home-dir /var/lib/devbox-kvm \
        --no-create-home --shell /usr/sbin/nologin \
        --comment 'DevBox KVM gateway' devbox-kvm
fi
if [ "$(id -u devbox-kvm)" = 0 ]; then
    echo 'devbox-kvm must be an unprivileged service account.' >&2
    exit 1
fi

# Never recurse through VM data, rewrite configuration, or delete retained state.
install -d -o devbox-kvm -g devbox-kvm -m 0700 /var/lib/devbox-kvm
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi

# Configuration and starting the listener are operator decisions. Upgrades stop
# the old service before replacing its executable, but never start it for you.
printf '%s\n' 'DevBox installed. Configure /etc/devbox-kvm/devbox-kvm.env, then run systemctl enable --now devbox-kvm.service. After an upgrade, start the service again.'
