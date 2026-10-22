#!/bin/bash
# Build candidate and verify all 10 streams byte-identical to native 2.6.0.
set -e
W=$(cd "$(dirname "$0")" && pwd)
cd /home/user/DevBox-Gateway-kvm
CGO_ENABLED=0 go test -overlay=$W/overlay.json -c -o $W/go-cand.test ./internal/rdp/avc
cd $W; mkdir -p cand; fail=0
while read name file w h qp; do for s in 1 4; do
  n=$name-${w}x$h-qp$qp-${s}s
  GOMAXPROCS=4 COMPARE_RUN=1 COMPARE_CASE=$name COMPARE_I420=$W/$file COMPARE_WIDTH=$w COMPARE_HEIGHT=$h COMPARE_QP=$qp COMPARE_SLICES=$s COMPARE_FRAMES=80 COMPARE_STREAM=$W/cand/$n.264 ./go-cand.test -test.run='^TestOpenH264Comparison$' >/dev/null
  cmp -s cand/$n.264 parity/$n-native26.264 || { echo "PARITY FAIL $n"; fail=1; }
done; done <<'C'
ShortClip clip1080p.i420 1920 1080 20
Road road1080.i420 1920 1080 20
Road road1080.i420 1920 1080 32
PDF pdf1080.i420 1920 1080 20
Road road720.i420 1280 720 20
C
[ $fail = 0 ] && echo "parity OK (10/10 identical to native)"
exit $fail
