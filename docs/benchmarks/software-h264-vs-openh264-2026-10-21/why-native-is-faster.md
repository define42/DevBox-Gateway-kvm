# Why native OpenH264 is faster on this comparison

The measured gap is primarily codec work within a frame. Native was already 2.02× faster with a single encoder worker; the four-worker gap was smaller, 1.68×. It cannot be explained primarily by slice-worker coordination. In the fresh Go profile, background GC marking accounted for 0.03 seconds out of 10.72 sampled seconds (0.28%).

The Go port began as a translation of OpenH264's scalar C reference paths and has selectively added SIMD. Its accelerated kernels are not a complete port of native OpenH264's optimized dispatch and assembly. Both implementations compile to machine code.

## Concrete differences verified in source and profiles

| Work | Current Go port | Native OpenH264 2.6.0 |
| --- | --- | --- |
| Four-block forward DCT | Two calls to an SSE2 kernel, each processing two blocks | AVX2 kernel processing four blocks together |
| CAVLC coefficient/run extraction | Scalar `CavlcParamCal_c` loop | `CavlcParamCal_sse42`, SIMD nonzero detection, POPCNT and packed PSHUFB extraction |
| Quantization | SSE2 standalone kernels; newer fused quantize/scan uses SSSE3 | AVX2 standalone quantization kernels |
| Half-pixel filtering | AVX2 widening loads, shifts/adds, and our own tail handling | Different AVX2 kernels using VPMADDUBSW/VPMADDWD and specialized 5/9/17-width paths |
| Small assembly calls | Slice/stride/span checks, argument marshaling into Go assembly ABI, then kernel | Raw-pointer/stride register arguments passed directly to native assembly |

These differences identify plausible sources of the extra work, **not a measured allocation of the total speed gap**. Wider vector instructions alone do not establish that native quantization beats our fused quantize/scan. Each candidate needs a component benchmark with identical inputs and output checks before a speedup is claimed.

Forward-transform evidence: `pkg/h264/internal/encoder/dct_simd_amd64.s:21` invokes the paired SSE2 kernel twice; native `codec/common/x86/dct.asm:900` implements the four-block AVX2 transform and `codec/encoder/core/src/encode_mb_aux.cpp:530` selects it. The fresh profiles actually execute `dctTwoT4SSE2Kernel` and `WelsDctFourT4_avx2`, respectively.

CAVLC evidence: `pkg/h264/internal/encoder/set_mb_syn_cavlc.go:108` calls scalar extraction; native `codec/encoder/core/src/set_mb_syn_cavlc.cpp:301` selects `CavlcParamCal_sse42`, whose packed extraction is in `codec/encoder/core/x86/coeff.asm:522`. Native's profile confirms that function runs. Our earlier scalar implementation beat a Go mask-enumeration attempt; that result did not compare against this native SIMD extraction scheme.

Filter evidence: `pkg/h264/internal/common/mc_avx2_amd64.s:25` versus native `codec/common/x86/mc_luma.asm:2988`. Both use AVX2, but their loads, arithmetic, batching and tail paths differ.

The [Go SAD wrapper disassembly](go-sad-wrapper.txt) shows bounds/stride checks, slice construction, eight stack argument stores, an ABI0 call, and the Go ABI register restoration. The [native SAD entry](native-sad-kernel.txt) accepts raw pointers/strides directly. Native also uses function tables; indirect dispatch by itself is not a Go-specific cost. Any optimization should amortize necessary checks over validated larger regions rather than remove safety checks indiscriminately.

## Fresh profile observations

- `WelsMdInterMbLoop` accounts for 94.78% of sampled Go CPU time including callees.
- Fractional motion refinement accounts for 19.68% including callees.
- CAVLC residual writing accounts for 11.66% including callees; its scalar parameter extraction alone is 3.54%.
- Four-block forward transform wrapper and callees account for 5.04%; its paired SSE2 kernel is 4.29% flat.
- Background GC marking is 0.28% including callees. This is not evidence of a large GC bottleneck.

These percentages overlap and must not be added together. Profile percentages from different implementations are fractions of different total runtimes; subtracting them does not quantify the wall-time gap.

See [Go flat profile](go-top.txt), [Go cumulative profile](go-cum.txt), [complete Go profile table](go-all.txt), [CAVLC source attribution](go-cavlc.txt), and [native flat profile](native-flat.txt).

## Profiling method and limits

Go: the existing comparison binary, one slice, GOMAXPROCS=4, identical preloaded I420 fixture; 16 warmup +240 measured frames; `-test.cpuprofile=go-1.pprof`. The profile includes setup and warmup. Total sampled CPU time: 10.72 seconds.

Native: existing optimized static OpenH264 library unchanged. Only the small benchmark harness was relinked with `g++ -O3 -pg -no-pie -std=c++17`, against that same `libopenh264.a`. One slice, same input, 16 warmup +480 measured frames. GNU gprof provides a 100 Hz flat PC histogram for the statically linked code. The library was not instrumented with `-pg`, so library call counts and call-graph attribution are unavailable; only flat samples are used. The profile includes setup and warmup. The histogram accumulated approximately 10.60 sampled seconds.

Hardware perf events were unavailable under the host's existing policy. No system profiling permissions were changed. Profiles ran serially, without concurrent agent builds/tests. These diagnostic runs are not new controlled speed comparisons; the six-pair results in [README.md](README.md) remain the timing evidence.

## Practical priorities

The clearest next candidates are a native-style four-block AVX2 forward transform, packed SIMD CAVLC extraction, and stronger half-pixel filter kernels. Larger batches and validated pointer-based internal interfaces may reduce repeated wrapper overhead. These are proposed investigations, not implemented changes or promised speedups.
