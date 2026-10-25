#!/bin/sh
set -eu

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi
# Preserve the service account and /var/lib/devbox-kvm, including on DEB purge.
# They contain persistent desktops, certificates and ownership information.
