#!/usr/bin/env bash
# Generate a private, persistent development LDAP identity; never copy upstream keys.
set -euo pipefail
cd -- "$(dirname -- "$0")"
umask 077
mkdir -p .tls
chmod 700 .tls
exec 9>.tls/.lock
flock 9
if [[ -e .tls/cert.pem || -e .tls/key.pem ]]; then
    if [[ ! -f .tls/cert.pem || ! -f .tls/key.pem ]]; then
        echo "Incomplete LDAP TLS identity in testldap/.tls; restore the certificate/key pair." >&2
        exit 1
    fi
    openssl x509 -in .tls/cert.pem -noout -checkend 0 >/dev/null
    cert_public=$(openssl x509 -in .tls/cert.pem -pubkey -noout)
    key_public=$(openssl pkey -in .tls/key.pem -pubout)
    if [[ "$cert_public" != "$key_public" ]]; then
        echo "LDAP certificate and key do not match." >&2
        exit 1
    fi
    exit 0
fi
tmp=$(mktemp -d .tls/generate.XXXXXX)
trap 'rm -rf -- "$tmp"' EXIT
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 365 \
    -subj '/CN=localhost' \
    -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
    -addext 'basicConstraints=critical,CA:FALSE' \
    -addext 'keyUsage=critical,digitalSignature,keyEncipherment' \
    -addext 'extendedKeyUsage=serverAuth' \
    -keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>"$tmp/openssl.log" || {
        cat "$tmp/openssl.log" >&2
        exit 1
    }
mv -- "$tmp/key.pem" .tls/key.pem
mv -- "$tmp/cert.pem" .tls/cert.pem
echo "Created local LDAP TLS identity in testldap/.tls."
