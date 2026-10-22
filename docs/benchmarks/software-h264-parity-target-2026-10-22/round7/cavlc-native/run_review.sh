#!/usr/bin/env bash
set -euo pipefail

review_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
go_tree=${CAVLC_GO_TREE:-"$review_dir/go"}
go_bin=${GO_BIN:-/snap/go/current/bin/go}
cxx_bin=${CXX_BIN:-g++}
native_root=${CAVLC_NATIVE_ROOT:-/home/define42/git/openh264}
native_lib=${CAVLC_NATIVE_LIB:-/tmp/openh264-comparison-20261021/native/libopenh264.a}
go_test=${CAVLC_GO_TEST:-"$review_dir/go-cavlc.test"}
export CAVLC_NATIVE_BINARY=${CAVLC_NATIVE_BINARY:-"$review_dir/native_cavlc"}
export CAVLC_NATIVE_FIXTURES=${CAVLC_NATIVE_FIXTURES:-"$review_dir/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt"}
mode=${MODE:-auto}
if [[ "$mode" != auto && "$mode" != scalar ]]; then
  echo "MODE must be auto or scalar" >&2
  exit 1
fi
go_debug=${CAVLC_GODEBUG:-}
if [[ "$mode" == scalar ]]; then
  go_debug="${go_debug:+$go_debug,}cpu.ssse3=off,cpu.popcnt=off"
fi

build_go() {
  (cd -- "$go_tree" && "$go_bin" test -c -o "$go_test" ./pkg/h264/internal/encoder)
}
build_native() {
  "$cxx_bin" -std=c++17 -O3 -DNDEBUG -m64 -DX86_ASM -DHAVE_AVX2 \
    -Wall -fno-strict-aliasing -fPIC -fstack-protector-all \
    -DGENERATED_VERSION_HEADER \
    -I"$native_root/codec/api/wels" -I"$native_root/codec/common/inc" \
    -I"$native_root/codec/encoder/core/inc" -I"$native_root/codec/encoder/plus/inc" \
    -I"$native_root/codec/processing/interface" \
    "$review_dir/native_cavlc.cpp" "$native_lib" -pthread -lm \
    -o "$CAVLC_NATIVE_BINARY"
}

case "${1:-}" in
  build)
    build_native
    build_go
    ;;
  build-go)
    build_go
    ;;
  parity)
    GOMAXPROCS=1 GODEBUG="$go_debug" "$go_test" \
      -test.run '^TestCavlcNative' -test.count=1 -test.v
    ;;
  bench)
    samples=${SAMPLES:-8}
    milliseconds=${MILLISECONDS:-400}
    filter=${BENCH_FILTER:-'.*'}
    output_dir=${OUTPUT_DIR:-"$review_dir/results-$mode"}
    mkdir -p -- "$output_dir"
    : > "$output_dir/go.txt"
    : > "$output_dir/native.txt"
    : > "$output_dir/native.log"
    bench_go() {
      GOMAXPROCS=1 GODEBUG="$go_debug" "$go_test" -test.run '^$' \
        -test.bench "^BenchmarkCavlcNativeFull/$filter$" \
        -test.cpu=1 -test.count=1 -test.benchtime="${milliseconds}ms" -test.benchmem \
        >> "$output_dir/go.txt"
    }
    bench_native() {
      "$CAVLC_NATIVE_BINARY" bench "$CAVLC_NATIVE_FIXTURES" "$mode" \
        "$milliseconds" 1 "^$filter$" \
        >> "$output_dir/native.txt" 2>> "$output_dir/native.log"
    }
    for ((sample=0; sample<samples; sample++)); do
      if ((sample % 2 == 0)); then
        bench_native
        bench_go
      else
        bench_go
        bench_native
      fi
    done
    echo "Results: $output_dir/go.txt $output_dir/native.txt"
    ;;
  *)
    echo "usage: $0 build|build-go|parity|bench" >&2
    exit 2
    ;;
esac
