#!/bin/sh
set -eu

# A running gateway launches workers from its executable's installed pathname.
# Stop it before unpacking a replacement so old gateways cannot start new-version
# workers. Preserve enablement; operators start the service after the upgrade.
# A first install has no existing unit and does not start anything.
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] &&
    systemctl is-active --quiet devbox-kvm.service; then
    systemctl stop devbox-kvm.service
fi
