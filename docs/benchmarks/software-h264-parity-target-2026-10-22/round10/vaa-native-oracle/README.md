# Independent native VAA output oracle

Source prepared without builds, native execution, or Go tests by the author.
All files are isolated here; no native, main-repository, or candidate file changed.
Root owns compilation/execution. This checks outputs, not performance.

## Native ABI and domain audit

Local source: /home/define42/git/openh264 (OpenH264 2.6.0).
Library: /tmp/openh264-comparison-20261021/native/libopenh264.a.

The seven-argument raw ABI is:

    void VAACalcSad_avx2(const uint8_t* cur, const uint8_t* ref,
                        int32_t width, int32_t height, int32_t stride,
                        int32_t* frameSad, int32_t* sad8x8);

Declarations are in codec/processing/src/vaacalc/vaacalculation.h:63,104,109.
C linkage is used for SSE2/AVX2; scalar linkage is WelsVP::VAACalcSad_c.

- vaa.asm:2396 implements AVX2. Width is rounded down to16 and height divided
  by16. The remaining16-pixel branches at2457/2480 support odd macroblock
  column counts, including a one-macroblock-wide image. Native's own unit test
  ProcessUT_VaaCalc.cpp:593 covers widths320,304,288,272 (even and odd counts).
- AVX2 input loads are unaligned: AVX2_Sad at2166 uses VMOVDQU and VPSADBW
  memory operands. Each32-pixel group and remaining16 group stays inside the
  completed macroblock width. Output is four int32 values per macroblock,
  raster macroblock order and quadrant order TL,TR,BL,BR; see the stores at2238.
- Native SSE2 has stricter input alignment: WELS_SAD_16x2_SSE2 at63 uses
  MOVDQA for both inputs. It is called only when both live pointers and stride
  are16-byte aligned. The harness's allocation base is32-byte aligned.
- Fixtures use positive widths/heights divisible by16, stride>=width, and
  disjoint inputs/outputs. This avoids empty-input differences and differing
  partial-width row advancement in native C/Go scalar versus AVX2. Negative,
  overlapping, malformed, partial-block and overflow semantics belong in the
  Go candidate's existing independent tests; no raw-native promises are made.
- Worst possible frame SAD is required to fit int32. Real-frame fixtures
  reach1920x1088; no signed-overflow assumption is required by the C oracle.

## Files and deterministic coverage

- generate_fixtures.py writes146 metadata records to fixtures.txt.
- native_vaa.cpp links to actual native C, AVX2, and eligible SSE2 symbols.
  It first checks all outputs against an independent pixel-by-pixel oracle,
  then emits the actual native AVX2 frame and quadrant outputs to native.txt.
  Four leading/trailing output canary words, surrounding frame canaries and
  complete input snapshots check output bounds and source immutability.
- vaacalcfuncs_native_oracle_test.go is an amd64/integration-tagged overlay
  compatible with round9: checks scalar, checked SSE2 and initialized dispatch.
- vaa_avx2_native_oracle_test.go additionally registers the candidate's public
  checked VAACalcSad_avx2. Install only through the candidate overlay.
- make_overlay.py creates Go overlay JSON without editing either source tree.

Shapes16x16,32x16,48x32,64x48,80x32,112x80,304x64,320x64 each have tight,
aligned-padded, and unaligned-padded layouts and six patterns (144 cases).
Two additional pseudorandom full-frame cases are1920x1088 and1904x1088, with
unaligned offsets3/11 and stride width+13: even120 and odd119 MB columns.
The six patterns are equal nonconstant frames, maximum absolute difference,
checkerboard complements, gradients, deterministic uint32 mixing, and distinct
macroblock/quadrant values. C++/Go independently regenerate bytes from x,y,
seed, plane and pattern; no RNG library or external clip is involved.

The text format is whitespace-delimited: header "vaa-native-v1 count", then
one record per fixture:

    name width height stride curOff refOff pattern seed frameSad count q0 q1 ...

The Go overlay rejects invalid metadata, duplicate names, invalid quadrant
ranges, mismatched frame sums and trailing/missing data. It compares every
quadruple and frame total, with exact visible input/output lengths plus
canaries. Its observed array ordering comes directly from native output.
These are not guard-page tests: candidate raw guard tests remain necessary.

## Root commands: native artifact first

    python3 /tmp/h264-parity-target-20261022/vaa-native-oracle/generate_fixtures.py
    c++ -O2 -std=c++17 -Wall -Wextra -Werror /tmp/h264-parity-target-20261022/vaa-native-oracle/native_vaa.cpp /tmp/openh264-comparison-20261021/native/libopenh264.a -lpthread -lm -o /tmp/h264-parity-target-20261022/vaa-native-oracle/native-vaa
    /tmp/h264-parity-target-20261022/vaa-native-oracle/native-vaa /tmp/h264-parity-target-20261022/vaa-native-oracle/fixtures.txt /tmp/h264-parity-target-20261022/vaa-native-oracle/native.txt

Require exit0; expected summary is146 native AVX2+C fixtures and96 aligned
native SSE2 fixtures. A failed run can leave incomplete native.txt; Go parser
also rejects it. Native runtime AVX2 support is checked before calling assembly.

## Root commands: baseline and candidate Go parity

    python3 /tmp/h264-parity-target-20261022/vaa-native-oracle/make_overlay.py /tmp/h264-parity-target-20261022/round9 /tmp/h264-parity-target-20261022/vaa-native-oracle/base-overlay.json
    VAA_NATIVE_ORACLE=/tmp/h264-parity-target-20261022/vaa-native-oracle/native.txt CGO_ENABLED=0 GOMAXPROCS=4 /snap/go/current/bin/go -C /tmp/h264-parity-target-20261022/round9 test -overlay /tmp/h264-parity-target-20261022/vaa-native-oracle/base-overlay.json -tags integration ./pkg/h264/internal/processing -run '^TestVAANativeOutputOracle$' -count=1 -v
    python3 /tmp/h264-parity-target-20261022/vaa-native-oracle/make_overlay.py /tmp/h264-parity-target-20261022/vaa-avx2-next /tmp/h264-parity-target-20261022/vaa-native-oracle/candidate-overlay.json --avx2
    VAA_NATIVE_ORACLE=/tmp/h264-parity-target-20261022/vaa-native-oracle/native.txt CGO_ENABLED=0 GOMAXPROCS=4 /snap/go/current/bin/go -C /tmp/h264-parity-target-20261022/vaa-avx2-next test -overlay /tmp/h264-parity-target-20261022/vaa-native-oracle/candidate-overlay.json -tags integration ./pkg/h264/internal/processing -run '^TestVAANativeOutputOracle$' -count=1 -v

Baseline runs3 paths (438 case/path comparisons); candidate runs4 (584).
The same candidate command with GODEBUG=cpu.avx2=off checks checked fallback
and dispatch against the already-generated native output. No native library
is linked to Go; the production build remains CGO_ENABLED=0.
