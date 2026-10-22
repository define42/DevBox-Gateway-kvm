#!/bin/bash
# usage: cg.sh BINARY(go-xxx|native_bench) CASE(road20|road32|pdf|clip) [frames]
W=$(cd "$(dirname "$0")" && pwd); b=$1; c=$2; f=${3:-20}
case $c in road20) i=road1080.i420; q=20;; road32) i=road1080.i420; q=32;; pdf) i=pdf1080.i420; q=20;; clip) i=clip1080p.i420; q=20;; esac
export GOMAXPROCS=1 GODEBUG=asyncpreemptoff=1 COMPARE_RUN=1 COMPARE_CASE=x COMPARE_I420=$W/$i COMPARE_QP=$q COMPARE_SLICES=1 COMPARE_FRAMES=$f
if [[ $b == native* ]]; then cmd=($W/$b $W/$i 1 $f); else cmd=($W/$b.test -test.run='^TestOpenH264Comparison$'); fi
valgrind --tool=cachegrind --cache-sim=no --cachegrind-out-file=$W/cg/$b-$c-$f.out "${cmd[@]}" 2>&1 | grep -o 'I *refs: *[0-9,]*' | tr -d ' ,' | sed "s/Irefs:/$b $c $f /"
