#!/bin/bash
# Serial correctness validation for the H.264 changes; logs per step.
W=$(cd "$(dirname "$0")" && pwd); V=$W/validation; cd /home/user/DevBox-Gateway-kvm
P="./pkg/h264/internal/common ./pkg/h264/internal/encoder ./internal/rdp/avc"
step() { name=$1; shift; echo "== $name: $*" > $V/$name.txt; if env "$@" >> $V/$name.txt 2>&1; then echo "PASS $name"; else echo "FAIL $name"; tail -30 $V/$name.txt; fi; }
step tests          go test -count=1 ./pkg/h264/... ./internal/rdp/...
step race           CGO_ENABLED=1 go test -race -count=1 $P
step avx2-off       GODEBUG=cpu.avx2=off go test -count=1 $P
step ssse3-off      GODEBUG=cpu.ssse3=off go test -count=1 $P
step all-off        GODEBUG=cpu.all=off go test -count=1 $P
for p in $P; do n=$(basename $p); step arm64-$n GOARCH=arm64 CGO_ENABLED=0 go test -c -o /dev/null $p; done
step build-386      GOARCH=386 CGO_ENABLED=0 go build ./pkg/h264/...
step vet-arm64      GOARCH=arm64 go vet ./pkg/h264/...
step gofmt          sh -c 'test -z "$(gofmt -l pkg/h264)"'
step lint           golangci-lint run ./pkg/h264/... ./internal/rdp/...
