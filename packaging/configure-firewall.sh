#!/bin/sh
# Called as root by systemd with the same EnvironmentFile as the gateway.
# Keep ownership records outside the service account's writable data directory.
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
LC_ALL=C
export LC_ALL
umask 077

state_dir=/var/lib/devbox-kvm-firewall
ufw_app_dir=/etc/ufw/applications.d
log() { printf '%s\n' "devbox-kvm firewall: $*" >&2; }
fail() { log "$*"; exit 1; }

case "${1:-}" in
    '') remove=false ;;
    --remove) remove=true ;;
    *) fail 'usage: configure-firewall [--remove]' ;;
esac
if [ "$remove" = false ]; then
    case "${FIREWALL_MANAGE:-true}" in
        true) ;;
        false) log 'automatic management disabled; existing rules are unchanged'; exit 0 ;;
        *) fail 'FIREWALL_MANAGE must be true or false' ;;
    esac
fi
[ "$(id -u)" = 0 ] || fail 'must run as root'
[ ! -L "$state_dir" ] || fail 'refusing symlink at firewall state directory'
install -d -o root -g root -m 0700 "$state_dir"
exec 9>"$state_dir/lock"
flock -x 9

# Do not source the environment file: systemd parses it before invoking us.
port=
if [ "$remove" = false ]; then
    listen=${LISTEN_ADDR:-127.0.0.1:8443}
    case "$listen" in
        *:*) host=${listen%:*}; port=${listen##*:} ;;
        *) fail 'LISTEN_ADDR must contain an explicit TCP port' ;;
    esac
    case "$port" in
        ''|*[!0-9]*|??????*) fail 'LISTEN_ADDR must use a numeric TCP port from 1 to 65535' ;;
    esac
    # Normalize leading zeroes without shell octal arithmetic.
    while [ "${port#0}" != "$port" ]; do port=${port#0}; done
    if [ -z "$port" ] || [ "$port" -gt 65535 ]; then
        fail 'LISTEN_ADDR port is outside 1 to 65535'
    fi
    case "$host" in
        localhost|localhost.localdomain|'[::1]'|'[0:0:0:0:0:0:0:1]') port= ;;
        127.*|'[::ffff:127.'*']')
            # Do not mistake a hostname such as 127.example.com for loopback.
            address=${host#\[::ffff:}
            address=${address%\]}
            case "$address" in *[!0-9.]*) ;; *) port= ;; esac
            ;;
    esac
    if [ -z "$port" ]; then
        log 'loopback listener; no incoming firewall rule is needed'
    fi
    case "$host" in
        *:*)
            case "$host" in
                \[*\]) ;;
                *) fail 'IPv6 LISTEN_ADDR must use [address]:port syntax' ;;
            esac
            ;;
    esac
fi

firewalld=false
if command -v firewall-cmd >/dev/null 2>&1; then
    if firewall-cmd --state >/dev/null 2>&1; then
        firewalld=true
    else
        result=$?
        [ "$result" = 252 ] || fail "cannot determine firewalld state (exit $result)"
    fi
fi
ufw_active=false
ufw_available=false
if command -v ufw >/dev/null 2>&1; then
    ufw_available=true
    status=$(ufw status) || fail 'cannot determine UFW state'
    case "$status" in
        'Status: active'*) ufw_active=true ;;
        'Status: inactive'*) ;;
        *) fail 'unrecognized UFW status' ;;
    esac
fi

# Apply both planes separately. Never reload firewalld, which could discard
# unrelated runtime rules. Record a rule only if it did not already exist.
fw_command() {
    plane=$1
    zone=$2
    operation=$3
    rule_port=$4
    set -- "--zone=$zone" "--$operation-port=$rule_port/tcp"
    if [ "$plane" = permanent ]; then set -- --permanent "$@"; fi
    firewall-cmd "$@"
}
fw_exists() {
    if fw_command "$1" "$2" query "$3" >/dev/null; then return 0; else result=$?; fi
    # An administrator may remove an old custom zone altogether. Its owned
    # rule is then absent; a missing newly requested zone remains an error.
    if [ "$result" = 112 ] && [ "${4:-}" = cleanup ]; then return 1; fi
    [ "$result" = 1 ] || fail "cannot query firewalld rule (exit $result)"
    return 1
}

zones=
add_zone() {
    case "$1" in
        ''|*[!a-zA-Z0-9_-]*) fail 'invalid firewalld zone name' ;;
    esac
    case " $zones " in
        *" $1 "*) ;;
        *) zones="${zones:+$zones }$1" ;;
    esac
}
if [ "$firewalld" = true ] && [ -n "$port" ]; then
    if [ -n "${FIREWALL_ZONE:-}" ]; then
        add_zone "$FIREWALL_ZONE"
    else
        default_zone=$(firewall-cmd --get-default-zone) || fail 'cannot read default firewalld zone'
        add_zone "$default_zone"
        active_zones=$(firewall-cmd --get-active-zones) || fail 'cannot read active firewalld zones'
        while IFS= read -r line; do
            case "$line" in
                ''|[[:space:]]*) continue ;;
            esac
            add_zone "$line"
        done <<EOF
$active_zones
EOF
    fi
fi

# Remove only our obsolete rules. Pre-existing administrator rules have no
# ownership marker and are never removed, including during package removal.
for marker in "$state_dir"/firewalld/*/*/*; do
    [ -f "$marker" ] || continue
    old_port=${marker##*/}
    parent=${marker%/*}
    old_zone=${parent##*/}
    parent=${parent%/*}
    old_plane=${parent##*/}
    if [ "$old_port" = "$port" ]; then
        if [ "$firewalld" = true ]; then
            case " $zones " in *" $old_zone "*) continue ;; esac
        elif [ -z "${FIREWALL_ZONE:-}" ] || [ "$old_zone" = "$FIREWALL_ZONE" ]; then
            # Preserve the saved rule if this manager is temporarily stopped.
            continue
        fi
    fi
    if [ "$firewalld" = true ]; then
        if fw_exists "$old_plane" "$old_zone" "$old_port" cleanup; then
            fw_command "$old_plane" "$old_zone" remove "$old_port" >/dev/null || fail 'cannot remove obsolete firewalld rule'
        fi
    elif [ "$old_plane" = permanent ]; then
        if command -v firewall-offline-cmd >/dev/null 2>&1; then
            if firewall-offline-cmd "--zone=$old_zone" "--remove-port=$old_port/tcp" >/dev/null; then
                :
            else
                result=$?
                [ "$result" = 112 ] || fail "cannot remove saved firewalld rule (exit $result)"
            fi
        else
            log "firewalld is inactive; retained owned rule $old_zone $old_port/tcp for later cleanup"
            continue
        fi
    else
        # CleanupOnExit=no can leave runtime rules in the kernel after the
        # daemon stops. Keep ownership until an active daemon can inspect them.
        log "firewalld is inactive; retained runtime ownership for $old_zone $old_port/tcp because kernel rules may still exist"
        continue
    fi
    rm -f "$marker"
done

for marker in "$state_dir"/ufw/*; do
    [ -f "$marker" ] || continue
    old_port=${marker##*/}
    if [ "$old_port" = "$port" ]; then continue; fi
    if [ "$ufw_available" = false ]; then
        log "UFW is unavailable; retained owned rule $old_port/tcp for later cleanup"
        continue
    fi
    # Application identity keeps a manual numeric allow rule independent.
    ufw --force delete allow "DevBox-KVM-$old_port" >/dev/null || fail 'cannot remove obsolete UFW rule'
    rm -f "$ufw_app_dir/devbox-kvm-$old_port" "$marker"
done

[ "$remove" = false ] || exit 0
[ -n "$port" ] || exit 0
if [ "$firewalld" = true ]; then
    for zone in $zones; do
        for plane in runtime permanent; do
            marker="$state_dir/firewalld/$plane/$zone/$port"
            if ! fw_exists "$plane" "$zone" "$port"; then
                install -d -o root -g root -m 0700 "${marker%/*}"
                # Journal intent before applying a previously absent rule, so
                # an interrupted command can be cleaned up on the next start.
                : >"$marker"
                fw_command "$plane" "$zone" add "$port" >/dev/null || fail 'cannot open configured port in firewalld'
            fi
        done
        log "firewalld allows $port/tcp in zone $zone (runtime and permanent)"
    done
fi
if [ "$ufw_active" = true ]; then
    marker="$state_dir/ufw/$port"
    profile="$ufw_app_dir/devbox-kvm-$port"
    if [ ! -f "$marker" ]; then
        if [ -e "$profile" ] || [ -L "$profile" ] || ufw app info "DevBox-KVM-$port" >/dev/null 2>&1; then
            fail "refusing to replace an unowned UFW profile: DevBox-KVM-$port"
        fi
    fi
    install -d -o root -g root -m 0700 "$state_dir/ufw"
    : >"$marker"
    cat >"$profile" <<EOF
[DevBox-KVM-$port]
title=DevBox KVM gateway
description=Managed shared HTTPS and RDP listener
ports=$port/tcp
EOF
    chmod 0644 "$profile"
    ufw allow "DevBox-KVM-$port" >/dev/null || fail 'cannot open configured port in UFW'
    log "UFW allows $port/tcp with application DevBox-KVM-$port"
fi
if [ "$firewalld" = false ] && [ "$ufw_active" = false ]; then
    log "no active firewalld or UFW; no incoming rule was added. Allow $port/tcp in your host or network firewall if required."
fi
