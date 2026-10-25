#!/bin/sh
set -eu

# Debian passes remove for uninstall and upgrade for replacement. RPM passes 0
# only when the last instance is removed; a positive count is an upgrade.
case "${1:-}" in
    remove|0)
        if [ -d /run/systemd/system ]; then
            systemctl stop devbox-kvm.service
            systemctl disable devbox-kvm.service
        fi
        if [ -x /usr/libexec/devbox-kvm/configure-firewall ]; then
            /usr/libexec/devbox-kvm/configure-firewall --remove
        fi
        ;;
esac
