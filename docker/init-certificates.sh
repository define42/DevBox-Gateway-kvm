#!/bin/sh
# Persistent development identities for the Compose gateway and LDAP service.
set -eu
umask 077

hostname=${GATEWAY_HOST:-localhost}
case "$hostname" in
    ''|*[!A-Za-z0-9.-]*|-*|.*)
        echo 'GATEWAY_HOST must be a DNS hostname or an IPv4 address, without a port.' >&2
        exit 1
        ;;
esac
case "$hostname" in
    *[!0-9.]*) host_san="DNS:$hostname"; host_check=-verify_hostname ;;
    *) host_san="IP:$hostname"; host_check=-verify_ip ;;
esac

temporary=
trap 'if [ -n "$temporary" ]; then rm -rf -- "$temporary"; fi' EXIT HUP INT TERM

identity() {
    directory=$1
    name=$2
    names=$3
    check=$4
    if [ -e "$directory/cert.pem" ] || [ -e "$directory/key.pem" ]; then
        if [ ! -f "$directory/cert.pem" ] || [ ! -f "$directory/key.pem" ]; then
            echo "Incomplete TLS identity in $directory; restore its certificate/key pair." >&2
            exit 1
        fi
        if ! openssl x509 -in "$directory/cert.pem" -noout -checkend 0 >/dev/null \
            || ! openssl verify -CAfile "$directory/cert.pem" -purpose sslserver \
                "$check" "$name" "$directory/cert.pem" >/dev/null; then
            echo "TLS identity in $directory is expired or does not cover $name; replace this TLS identity." >&2
            exit 1
        fi
        cert_public=$(openssl x509 -in "$directory/cert.pem" -pubkey -noout)
        key_public=$(openssl pkey -in "$directory/key.pem" -pubout)
        if [ "$cert_public" != "$key_public" ]; then
            echo "TLS certificate and private key do not match in $directory." >&2
            exit 1
        fi
        return
    fi
    temporary=$(mktemp -d "$directory/.generate.XXXXXX")
    if ! openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 365 \
        -subj "/CN=$name" -addext "subjectAltName=$names" \
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
    echo "Created TLS identity for $name."
}

identity /certs/gateway "$hostname" "DNS:localhost,IP:127.0.0.1,$host_san" "$host_check"
identity /certs/ldap ldap DNS:ldap -verify_hostname
# The gateway receives only LDAP's public certificate, never its private key.
cp /certs/ldap/cert.pem /certs/gateway/ldap-ca.pem
cp /usr/local/share/gokvm-ldap/config.cfg /certs/ldap/config.cfg
echo 'Compose TLS identities and LDAP configuration are ready.'
