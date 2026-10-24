# Final component benchmark summary

This summary uses the archived `dct-components/benchstat.txt`, `cavlc-fixed-components/benchstat.txt`, and `motion-components/benchstat.txt`. The initial CAVLC timing is superseded; the CAVLC row below includes the malformed-cache fallback fix. Each baseline/candidate comparison has six samples. “Improved” and “regressed” follow benchstat's significant `sec/op` comparisons; `~` is counted as inconclusive. Gain ranges are reductions in time per operation among significant improvements only.

| Group | Cases | Improved | Regressed | Inconclusive | Significant time reduction |
| --- | ---: | ---: | ---: | ---: | ---: |
| DCT builtin, strides 16/64/2048 | 3 | 3 | 0 | 0 | 35.90–40.89% |
| DCT scalar/wrapped callback controls | 6 | 0 | 0 | 6 | — |
| CAVLC complete macroblocks, final fixed candidate | 8 | 4 | 0 | 4 | 12.90–24.73% |
| Fractional search, wide blocks (16×16, 16×8, 8×16, 8×8) | 16 | 14 | 0 | 2 | 9.29–27.51% |
| Fractional search, narrow blocks (8×4, 4×4, 4×8) | 12 | 0 | 0 | 12 | — |
| Fractional search callback/feature controls | 8 | 0 | 0 | 8 | — |
| Integer search | 40 | 29 | 0 | 11 | 7.98–33.09% |
| Total | 93 | 50 | 0 | 43 | — |

The four significant CAVLC results are inter/sparse (12.90%), intra16/dense (21.59%), intra16/mixed (13.48%), and intra16/sparse (24.73%). Inter/dense, inter/mixed and both zero distributions are inconclusive in the final comparison.

The two inconclusive wide fractional cases are 16×16/stride2048/motionfalse and 8×16/stride32/motiontrue. The 11 inconclusive integer cases include all eight early-exit cases and three many-candidate cases: 16×16/stride2048, 8×4/stride2048 and 4×4/stride2048.

All 1,116 raw samples across these six baseline/candidate result files report **0 B/op and 0 allocs/op**. The 186 memory-metric rows in the three benchstat reports agree. No significant component regressions were observed, but inconclusive results do not establish equivalence. These are isolated component measurements; whole-codec timing is reported separately.
