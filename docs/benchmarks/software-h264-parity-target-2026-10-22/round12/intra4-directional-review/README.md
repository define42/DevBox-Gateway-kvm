# Directional I4 predictors: component results

The HD/HU/VR SSE2 candidate passes this component gate. All six dispatched predictor cases improve **36.04–49.73%**. The complete `WelsMdI4x4Fast` caller has **10 significant wins, 12 neutral cases, and no significant regressions**; its descriptive geomean is **3.06% lower time**. Whole-codec benefit and native performance parity are not established by these measurements.

| Scope | Cases | Significant improvements | Neutral | Significant regressions |
|---|---:|---:|---:|---:|
| Checked HD/HU/VR dispatch | 6 | 6, by 36.04–49.73% | 0 | 0 |
| Unchanged scalar predictor controls | 6 | 0 | 6 | 0 |
| Complete I4 macroblock caller | 22 | 10, by 3.41–7.01% | 12 | 0 |

All dispatched results have benchstat p=0.002. The six reductions are HD 41.72%/40.70%, HU 48.39%/49.73%, and VR 39.19%/36.04%, for strides 32/1984 respectively. All 408 raw timing samples report zero bytes and zero allocations per operation. [Benchstat](benchstat.txt), [baseline samples](base.txt), [candidate samples](after.txt), and the independently parsed [audit](audit.json) preserve the details.

The caller geomean uses the geometric mean of candidate/baseline median ratios over all 22 caller cases; it has no separate significance test. Benchstat's **11.28% combined geomean mixes predictor and caller benchmarks**, so it is not a caller or frame result. The two `IsolatedFirstExit` cases execute only the first block with DC128 and are neutral. `IsolatedFull` can reach directional modes after reconstruction supplies internal neighbors; its 1984-stride 3.41% win is not a zero-exposure control. Wrapped-SAD and SATD controls still use the changed predictors.

## Exact comparison and benchmark scope

The measured baseline is `../intra4-directional-round11-base`, copied from frozen `../round11`. The candidate is `../intra4-directional-next`, prepared from round11-next. The two measured trees have identical `intra4_directional_bench_test.go` and `intra4_sad_bench_test.go`; their remaining H264 differences are exactly the three candidate production files and three new test files. All eight candidate files still match the frozen [source manifest](../intra4-directional-next-README.txt).

[The runner](../measure_intra4_directional.py) uses six alternating baseline/candidate pairs, CPU 2 affinity, GOMAXPROCS=4, CGO_ENABLED=0, and 200 ms per case. Host: Linux amd64, Intel Core i7-7700HQ at nominal 2.80 GHz. Each build has 34 cases with six samples each and six PASS records.

The predictor benchmark times checked callbacks selected by the actual CPU dispatcher, plus scalar controls, at strides 32 and 1984. The unchanged complete-caller fixture runs `WelsMdI4x4Fast`, including prediction, cost calculation, transforms and reconstruction when not exited early, at source/reference strides 32/32 and 1920/1984. Its 11 cases per stride cover QP20/32, flat/smooth/noise content, neighbor masks 15/7/2/0, full/first-block exit, wrapped SAD and SATD. The same timed reset restores reconstructed pixels and the mode cache. Neither benchmark is a raw assembly-only measurement or a full frame encode.

## Validation and remaining limits

The full encoder package passed for [baseline](base-tests.txt) and [candidate](after-tests.txt). Candidate directional tests also passed [race](after-race-tests.txt) and [CPU features disabled](after-features-off-tests.txt). These tests compare unchanged scalar predictors and complete caller state, including aliases, signed/overlapping strides, exact reads/stores, malformed-input panic order, custom callbacks and allocations. Logs are non-verbose package summaries; they do not establish that every optional external-data test ran.

This runner does not perform a new native encoder comparison, stream-hash/decoder gate, generic-architecture build, full-frame benchmark, or RPP/network measurement. Six samples on one host support proceeding to those gates, not a claim of native-equivalent encoding speed. Root has selected the candidate provisionally for round12, pending combined frame/native validation. No production changes were made during this results audit.
