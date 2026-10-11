#!/bin/sh
# Keep LDAP's private identity separate from the public certificate trust volume.
set -eu
umask 077

directory=/app/config
temporary=
public_temporary=
trap 'if [ -n "$temporary" ]; then rm -rf -- "$temporary"; fi; if [ -n "$public_temporary" ]; then rm -f -- "$public_temporary"; fi' EXIT HUP INT TERM

if [ -e "$directory/cert.pem" ] || [ -e "$directory/key.pem" ]; then
    if [ ! -f "$directory/cert.pem" ] || [ ! -f "$directory/key.pem" ]; then
        echo "Incomplete LDAP TLS identity in $directory; restore its certificate/key pair." >&2
        exit 1
    fi
    if ! openssl x509 -in "$directory/cert.pem" -noout -checkend 0 >/dev/null \
        || ! openssl verify -CAfile "$directory/cert.pem" -purpose sslserver \
            -verify_hostname ldap "$directory/cert.pem" >/dev/null; then
        echo 'LDAP TLS identity is expired or does not cover ldap; replace its certificate/key pair.' >&2
        exit 1
    fi
    cert_public=$(openssl x509 -in "$directory/cert.pem" -pubkey -noout)
    key_public=$(openssl pkey -in "$directory/key.pem" -pubout)
    if [ "$cert_public" != "$key_public" ]; then
        echo 'LDAP TLS certificate and private key do not match.' >&2
        exit 1
    fi
else
    temporary=$(mktemp -d "$directory/.generate.XXXXXX")
    if ! openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 365 \
        -subj '/CN=ldap' -addext 'subjectAltName=DNS:ldap' \
        -addext 'basicConstraints=critical,CA:FALSE' \
        -addext 'keyUsage=critical,digitalSignature,keyEncipherment' \
        -addext 'extendedKeyUsage=serverAuth' \
        -keyout "$temporary/key.pem" -out "$temporary/cert.pem" 2>"$temporary/openssl.log"; then
        cat "$temporary/openssl.log" >&2
        exit 1
    fi
    mv "$temporary/key.pem" "$directory/key.pem"
    mv "$temporary/cert.pem" "$directory/cert.pem"
    rm -rf -- "$temporary"
    temporary=
    echo 'Created LDAP TLS identity.'
fi

# Export only the public certificate. The gateway never mounts /app/config.
public_temporary=$(mktemp /ldap-ca/.cert.XXXXXX)
cp "$directory/cert.pem" "$public_temporary"
chmod 0644 "$public_temporary"
mv "$public_temporary" /ldap-ca/cert.pem
public_temporary=

# Preserve local account/configuration changes across container restarts.
if [ ! -e "$directory/config.cfg" ]; then
    cp /usr/local/share/gokvm-ldap/config.cfg "$directory/config.cfg"
fi
exec /app/glauth -c "$directory/config.cfg" "$@"
