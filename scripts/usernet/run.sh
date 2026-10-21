#!/usr/bin/env bash
# Local-only integration traffic; no KVM, TAP, root or external peers required.
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
if [[ $# != 1 ]]; then
    echo "usage: $0 NEW_RESULT_DIRECTORY" >&2
    exit 2
fi
result_dir=$1
mkdir -- "$result_dir"
result_dir=$(cd -- "$result_dir" && pwd)
source_dir=${USERNET_SOURCE_DIR:-$repo_dir}
go_bin=${GO:-go}
repetitions=${USERNET_BENCH_COUNT:-10}
processors=${GOMAXPROCS:-4}
if [[ ! $repetitions =~ ^[1-9][0-9]*$ || ! $processors =~ ^[1-9][0-9]*$ ]]; then
    echo "USERNET_BENCH_COUNT and GOMAXPROCS must be positive integers" >&2
    exit 2
fi
export GOMAXPROCS=$processors
export DEVBOX_USERNET_STRESS_DURATION=${DEVBOX_USERNET_STRESS_DURATION:-10s}
cd -- "$source_dir"
{
    date -u +%FT%TZ
    uname -a
    "$go_bin" version
    "$go_bin" env GOOS GOARCH GOTOOLCHAIN
    if [[ -f /proc/cpuinfo ]]; then awk '/model name/{print; exit}' /proc/cpuinfo; fi
    printf 'source_dir=%s\nGOMAXPROCS=%s\ncount=%s\nstress_duration=%s\n' \
        "$source_dir" "$processors" "$repetitions" "$DEVBOX_USERNET_STRESS_DURATION"
    printf 'open_file_limit='; ulimit -n
    for limit in /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max; do
        if [[ -r $limit ]]; then printf '%s=' "$limit"; cat -- "$limit"; fi
    done
    git rev-parse HEAD 2>/dev/null || true
    sha256sum go.mod go.sum internal/usernet/*.go
} > "$result_dir/environment.txt"
"$go_bin" test -tags=integration ./internal/usernet -run '^$' \
    -bench '^BenchmarkNetworkTCP$' -benchmem -benchtime=1s \
    -count="$repetitions" -cpu="$processors" -timeout=5m | tee "$result_dir/bench.txt"
"$go_bin" test -tags=integration ./internal/usernet \
    -run '^TestNetwork(MalformedFloodRecovery|ConnectionSaturationRecovery)$' \
    -count=1 -timeout=12m -v | tee "$result_dir/stress.txt"
python3 "$repo_dir/scripts/usernet/summarize.py" "$result_dir/bench.txt" > "$result_dir/summary.json"
