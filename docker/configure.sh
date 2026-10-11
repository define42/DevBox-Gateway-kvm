#!/bin/sh
# Prepare local Compose settings without hard-coding the host's KVM group.
set -eu
cd -- "$(dirname -- "$0")/.."
umask 077

if [ ! -c /dev/kvm ]; then
    echo '/dev/kvm is missing; enable KVM on this Linux host before starting the gateway.' >&2
    exit 1
fi
kvm_gid=$(stat -c '%g' /dev/kvm)
source_file=.env
if [ ! -f "$source_file" ]; then
    source_file=.env.example
fi

temporary=$(mktemp .env.tmp.XXXXXX)
trap 'rm -f -- "$temporary"' EXIT HUP INT TERM
# Do not source .env: it is Compose configuration, not a shell script.
awk -v gid="$kvm_gid" '
    /^[[:space:]]*(export[[:space:]]+)?KVM_GID[[:space:]]*[:=]/ {
        if (!written) {
            print "KVM_GID=" gid
            written = 1
        }
        next
    }
    { print }
    END {
        if (!written) print "KVM_GID=" gid
    }
' "$source_file" > "$temporary"
mv -- "$temporary" .env
printf 'Configured .env with KVM_GID=%s.\n' "$kvm_gid"
