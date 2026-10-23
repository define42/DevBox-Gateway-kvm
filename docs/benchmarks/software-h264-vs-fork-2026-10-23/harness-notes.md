# Harness protocol

Both harnesses load the same packed I420 input into memory before encoding. The Go overlay calls the production `newCodecEncoder` constructor with explicit 60 fps, then calls its production `encodeDamage` method. The native executable initializes the corresponding current-fork API parameters explicitly. Both read back effective settings, reject a changed core policy, and emit an encoder-only `PARAMS` JSON object for the runner to compare in full.

Environment variables shared by both harnesses:

| Variable | Default | Meaning |
| --- | --- | --- |
| `COMPARE_I420` | Required | Packed Y, U, V input file containing one or more complete frames |
| `COMPARE_WIDTH`, `COMPARE_HEIGHT` | 1920, 1080 | Even source dimensions |
| `COMPARE_SLICES` | 1 | Exactly 1 or 4 slices and threads |
| `COMPARE_QP` | 20 | Fixed quantizer, 0–51 |
| `COMPARE_FRAMES` | 60 | Number of measured frames after warmup |
| `COMPARE_WARMUP` | 16 | Number of excluded warmup frames |
| `COMPARE_CASE` | Clip | Benchmark label |
| `COMPARE_HINTS` | None | Optional per-input-frame unchanged macroblock masks |
| `COMPARE_DURATIONS` | None | CSV destination for every measured frame |
| `COMPARE_STREAM` | None | Annex-B destination containing warmup and measured frames, for a separate validation invocation |

The Go test additionally requires `COMPARE_RUN=1`. Run its compiled test binary with `-test.run '^TestOpenH264Comparison$' -test.count=1`. The native executable accepts the same environment with no positional arguments. For compatibility it also accepts `I420 SLICES [TIMED_FRAMES [OUTPUT.264]]`; those arguments override their environment equivalents.

Each mask contains exactly `ceil(width/16) * ceil(height/16)` bytes in macroblock row-major order, with 1 certifying unchanged source samples and 0 carrying no assertion. There must be exactly one mask per input frame, and only bytes 0 or 1 are accepted. The first encoded frame receives no hints. Later input and mask indices both cycle modulo the input-frame count. Mask zero therefore describes the wrap from the last input frame back to the first. The fixture generator must verify mask truth, including chroma and cropped border macroblocks. No motion hints are supplied.

The measured wall interval includes the codec call and its allocation/copy into a caller-owned compressed output buffer. Process CPU time brackets the same operation and includes all process threads. Setup, file I/O, input preparation, hint conversion, statistics, and CSV writing occur outside those intervals. Warmup is excluded. The percentile is nearest-rank p95 across measured frame durations. Means include every measured frame, including any GC or scheduler delay occurring inside an encode call.

`COMPARE_STREAM` retains output access units until writing them after encoding. Its invocations are for output validation only because retention changes memory behavior. Performance invocations leave it unset; optional CSV logging uses buffers allocated before encoding and writes the file only after all frames finish. Native version and run metadata are printed outside `PARAMS` so that the effective encoder dictionary is stable between validation and measurement.
