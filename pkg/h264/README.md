# OpenH264 for Go

A pure Go port of [OpenH264](https://github.com/cisco/openh264): an H.264
Constrained Baseline encoder plus a decoder for Constrained Baseline, Main and
High (progressive, 4:2:0, 8-bit, CAVLC and CABAC). It uses no cgo and no
assembly, so it cross-compiles to every platform Go supports.

The port is a close translation of the C++ reference (`_c`) code paths, and its
output is **bit-exact** with the C library:

* the decoder reproduces the SHA1 of the decoded YUV for all 51 conformance
  streams in `test/api/decoder_test.cpp`
  (`internal/decoder/conformance_test.go`);
* the encoder reproduces the bitstream SHA1 of all 16 configurations in
  `test/api/encoder_test.cpp` (`internal/encoder/encoder_conformance_test.go`).

## Usage

```go
import (
	openh264 "github.com/define42/DevBox-Gateway-kvm/pkg/h264"
)

// Encoding
p, _ := openh264.DefaultEncoderParams(640, 480, 1_000_000, 30) // *api.SEncParamExt
enc, _ := openh264.NewEncoder(p)
defer enc.Close()
f := openh264.NewFrame(640, 480) // fill f.Y, f.U, f.V (I420)
bitstream, frameType, err := enc.Encode(f)

// Decoding
dec, _ := openh264.NewDecoder(nil)
defer dec.Close()
frame, err := dec.Decode(accessUnit) // frame may be nil (reordering delay)
rest, err := dec.Flush()             // drain at end of stream
```

The full C++-style API is available too: `openh264.CreateDecoder()` /
`openh264.CreateEncoder()` return `api.ISVCDecoder` / `api.ISVCEncoder` with
the same methods, parameters and options as `codec/api/wels/codec_api.h`.
`SetOption`/`GetOption` take a pointer of the type the C API expects; the
mapping is listed in `api/doc.go`.

## Command-line tools

`cmd/h264dec` and `cmd/h264enc` are ports of the `h264dec` and `h264enc`
console programs. They take the same arguments and `.cfg` files:

```
go run ./cmd/h264enc ../testbin/welsenc.cfg -org in.yuv -bf out.264
go run ./cmd/h264dec out.264 out.yuv
```

## Layout

| Package               | Ported from                                |
|-----------------------|--------------------------------------------|
| `.` (`openh264`)      | factory functions + idiomatic wrapper      |
| `api`                 | `codec/api/wels`                           |
| `internal/common`     | `codec/common`                             |
| `internal/decoder`    | `codec/decoder`                            |
| `internal/encoder`    | `codec/encoder`                            |
| `internal/processing` | `codec/processing`                         |
| `cmd/h264dec`, `cmd/h264enc` | `codec/console`                     |

Internal code keeps the C identifiers so each Go file can be compared with its
C++ counterpart. The conventions are in [PORTING.md](PORTING.md).

## Differences from the C library

* **Selective encoder concurrency.** Fixed-slice, CAVLC camera encoding with
  rate control, load balancing, background detection, and adaptive quantization
  disabled uses bounded Go workers. Decoding and other encoder modes run
  sequentially. With the thread count set to "auto" (0), the encoder requests
  `runtime.NumCPU()` and then clamps it to four, as C does, so set an explicit
  count if you need output that is the same on every machine.
* **No SIMD.** Expect it to be several times slower than the assembly-optimized
  C build.
* **Memory safety.** Where the C code would read or write out of bounds on
  corrupt input, the Go code takes the existing error path instead. These
  places are commented in the source.
* **One `unsafe` use** (`internal/encoder/paraset_strategy.go`). It reproduces
  an out-of-range array index in the C parameter-set-id strategy and stays
  within the enclosing struct.

## Testing

```
go test ./...
```

The optional conformance tests read streams from the repository-root `res/`
directory and skip when that external data set is absent.

## License

BSD 2-Clause, the same as OpenH264 (see [LICENSE-OpenH264](../../LICENSE-OpenH264)).
