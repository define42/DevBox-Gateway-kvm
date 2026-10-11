#!/usr/bin/env bash
# The minimal GLAuth image has no health-check client; verify TLS from the host.
set -euo pipefail
cd -- "$(dirname -- "$0")"
port=${1:-1636}
if [[ ! "$port" =~ ^[0-9]{1,5}$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    echo "Invalid LDAP port: $port" >&2
    exit 1
fi
for ((attempt = 0; attempt < 20; attempt++)); do
    if timeout 2 openssl s_client -connect "127.0.0.1:$port" \
        -CAfile .tls/cert.pem -verify_return_error -verify_ip 127.0.0.1 \
        </dev/null >/dev/null 2>&1; then
        echo "LDAP ready at ldaps://127.0.0.1:$port (certificate verified)."
        exit 0
    fi
    sleep 1
done
echo "LDAP did not become ready; inspect docker compose -f testldap/compose.yaml logs ldap." >&2
exit 1
