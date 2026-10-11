#!/usr/bin/env bash
# Every disk created here is disposable and stays below a fresh result directory.
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
if [[ $# != 1 ]]; then
    echo "usage: $0 NEW_RESULT_DIRECTORY" >&2
    exit 2
fi
result_dir=$1
mkdir -- "$result_dir"
result_dir=$(cd -- "$result_dir" && pwd)
scratch_dir="$result_dir/scratch"
mkdir -- "$scratch_dir"
cleanup() {
    if [[ ${KEEP_DISKS:-0} == 0 ]]; then rm -rf -- "$scratch_dir"; fi
}
trap cleanup EXIT

go_bin=${GO:-go}
kernel=${KERNEL:-"$repo_dir/bzImage"}
size_mib=${SIZE_MIB:-64}
ops=${OPS:-4096}
repetitions=${REPETITIONS:-6}
seed=${SEED:-42}
for value in "$size_mib" "$ops" "$repetitions" "$seed"; do
    if [[ ! $value =~ ^[0-9]+$ ]]; then echo "numeric settings must be nonnegative integers" >&2; exit 2; fi
done
if ((size_mib < 1 || ops < 1 || repetitions < 1)); then echo "size, ops, and repetitions must be positive" >&2; exit 2; fi
size=$((size_mib * 1048576))
cd -- "$repo_dir"
CGO_ENABLED=0 "$go_bin" build -o "$result_dir/workload" ./scripts/diskbench/workload
if [[ -n ${RUNNER:-} ]]; then
    cp -- "$RUNNER" "$result_dir/runner"
else
    CGO_ENABLED=0 "$go_bin" build -o "$result_dir/runner" ./scripts/diskbench/runner
fi
{
    date -u +%FT%TZ
    uname -a
    "$go_bin" version
    git rev-parse HEAD
    git status --short
    sha256sum "$result_dir/runner" "$result_dir/workload" "$kernel"
    df -T "$result_dir"
    printf 'size_mib=%s ops=%s repetitions=%s seed=%s\n' "$size_mib" "$ops" "$repetitions" "$seed"
    printf 'runner_override=%s\n' "${RUNNER:-none}"
} > "$result_dir/environment.txt"
git diff -- internal/disk internal/virtio/blk.go internal/virtio/modern.go > "$result_dir/runtime.diff"

# Nonzero prepopulation avoids sparse/zero-cluster read shortcuts. All host and
# guest files are created on the result directory's filesystem.
python3 - "$scratch_dir/seed.raw" "$size_mib" <<'PY'
import os, sys
with open(sys.argv[1], "xb", buffering=0) as f:
    for _ in range(int(sys.argv[2])):
        f.write(b"\xa5" * 1048576)
    os.fsync(f.fileno())
PY

for direct in false true; do
    label=host-buffered
    if [[ $direct == true ]]; then label=host-direct; fi
    cp --reflink=never -- "$scratch_dir/seed.raw" "$scratch_dir/$label.raw"
    echo "Running $label" >&2
    "$result_dir/workload" -path "$scratch_dir/$label.raw" -label "$label" \
        -size "$size" -ops "$ops" -repetitions "$repetitions" -seed "$seed" -direct="$direct" \
        > "$result_dir/$label.log"
    sed -n '/^{"label":/p' "$result_dir/$label.log" >> "$result_dir/results.jsonl"
done

mkdir -p -- "$scratch_dir/initrd/dev" "$scratch_dir/initrd/proc" "$scratch_dir/initrd/sys"
cp -- "$result_dir/workload" "$scratch_dir/initrd/init"
for format in raw qcow2; do
    label="guest-$format"
    if [[ $format == raw ]]; then
        cp --reflink=never -- "$scratch_dir/seed.raw" "$scratch_dir/$label.raw"
    else
        qemu-img convert -f raw -O qcow2 -o cluster_size=65536 \
            "$scratch_dir/seed.raw" "$scratch_dir/$label.qcow2"
    fi
    python3 - "$scratch_dir/initrd/diskbench.json" "$label" "$size" "$ops" "$repetitions" "$seed" <<'PY'
import json, sys
path, label, size, ops, repetitions, seed = sys.argv[1:]
with open(path, "w") as f:
    json.dump(dict(Path="/dev/vda", Label=label, Size=int(size), Operations=int(ops),
                   Repetitions=int(repetitions), Direct=True, Seed=int(seed)), f)
PY
    (cd -- "$scratch_dir/initrd" && find . -print0 | cpio --null -o --format=newc --quiet) \
        > "$result_dir/$label.initrd"
    echo "Running $label" >&2
    "$result_dir/runner" -kernel "$kernel" -initrd "$result_dir/$label.initrd" \
        -disk "$scratch_dir/$label.$format" -serial-log "$result_dir/$label.serial.log" \
        -timeout "${TIMEOUT:-10m}" > "$result_dir/$label.jsonl"
    cat -- "$result_dir/$label.jsonl" >> "$result_dir/results.jsonl"
done

# Compare every logical byte across host and guest after closing the VM and
# reopening its image. Verification is outside all timing intervals.
qemu-img check -f qcow2 "$scratch_dir/guest-qcow2.qcow2" > "$result_dir/qcow2-check.txt"
qemu-img convert -f qcow2 -O raw "$scratch_dir/guest-qcow2.qcow2" "$scratch_dir/guest-qcow2.raw"
for label in host-direct guest-raw guest-qcow2; do
    cmp -- "$scratch_dir/host-buffered.raw" "$scratch_dir/$label.raw"
done
sha256sum "$scratch_dir/host-buffered.raw" "$scratch_dir/host-direct.raw" \
    "$scratch_dir/guest-raw.raw" "$scratch_dir/guest-qcow2.raw" > "$result_dir/disk-sha256.txt"
python3 "$repo_dir/scripts/diskbench/summarize.py" "$result_dir/results.jsonl" \
    > "$result_dir/summary.md"
cat -- "$result_dir/summary.md"
