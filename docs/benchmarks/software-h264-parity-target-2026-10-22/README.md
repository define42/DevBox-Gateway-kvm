# Software H.264 versus native OpenH264

This review implemented and measured thirty-four optimization groups in the Go encoder. **Native speed parity has not been achieved.** Encoding settings and output remain unchanged; all ten checked streams match both the original Go baseline and local native OpenH264 byte for byte.

The [latest report](round12/README.md) contains current native comparisons, complete `EncodeDamage` timings, correctness evidence, remaining gaps, and the cumulative source patch. The full-path comparison includes original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`, frozen round eleven and current source. The codec comparison uses round eleven/current/native to isolate the directional-predictor change; distinguish incremental and cumulative gains.

| Milestone | Retained improvements added |
| --- | --- |
| [Round three](round3/README.md) | Packed four-block inverse transforms; reused quarter-refinement validation; packed narrow SAD rows; packed eight-wide four-candidate SAD; large block copies. |
| [Round four](round4/README.md) | Paired AVX2 quantization/scan; inter-luma bookkeeping; direct checked SAD pointers. |
| [Round five](round5/README.md) | Sixteen-wide SAD scheduling; fixed-width intra predictor stores; packed CAVLC level table; SSSE3 chroma filtering; AVX2 quantization/max; SSSE3 intra variance. |
| [Round six](round6/README.md) | Paired AVX2 center filters; smaller single-block transform calls; fused SSE2 chroma DC Hadamard quantization. |
| [Round seven](round7/README.md) | Complete plane prediction; four-candidate quarter-pixel scoring; fixed-shape SATD dispatch; motion-cost locals; known-zero CAVLC. |
| [Round eight](round8/README.md) | Paired initial SAD; paired half-pixel SATD; shorter chroma keep/drop scoring. |
| [Round nine](round9/README.md) | SSE2 intra16 DC; lazy skip DCT/quantization fusion; intra16 AC quantization/scan fusion; guarded chroma QP thresholds. |
| [Round ten](round10/README.md) | Single-call center filters with compact stack scratch; AVX2 SAD-only VAA. |
| [Round eleven](round11/README.md) | Smaller four-candidate SAD arguments; SSE2 macroblock motion-vector fill. |
| [Round twelve](round12/README.md) | SSE2 horizontal-down, horizontal-up and vertical-right I4 predictors. |

Rejected or held experiments are recorded in their reports, including macroblock metadata layout, SATD scheduling, P-skip closure removal, smaller block copies, and CAVLC flush rewrites. Microbenchmark wins were not treated as proof of full-frame gains.

The [round-thirteen held experiments](round13/README.md) preserve the rejected compact quarter-refinement attempts and tuned CAVLC emitter. They add no retained optimization group; round twelve remains the latest implementation milestone.

These results describe the local Intel i7-7700HQ, Go 1.27.3 and matched native OpenH264 2.6.0. They do not establish equal speed across CPUs, encoder settings or live RDP sessions.
