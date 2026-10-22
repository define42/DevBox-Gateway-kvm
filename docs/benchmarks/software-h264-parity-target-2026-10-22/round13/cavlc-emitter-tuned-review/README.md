# Tuned CAVLC emitter — held

The single density-gated follow-up still fails the predeclared caller controls. It is **not merged**. All correctness gates passed, but 9 of 87 timing cases regressed, including complete-macroblock zero/density controls. No full-frame or native timing run followed this candidate, and this archive makes no frame-rate or parity-speed claim.

The tune attempts the checked assembly emitter only inside the existing `totalCoeffs > 3` branch and for end14/15. Assembly, ABI, table semantics, fallback and commit behavior are unchanged from the first held emitter. End3 blocks are excluded by this source gate yet several public end3 controls still regress; this does not establish a specific compiler/layout cause. The exact frozen and formatted sources are preserved under [source-delta](source-delta/).

## Complete timing outcome

Six alternating base/candidate pairs, pinned CPU2, GOMAXPROCS=1, 200ms per case. The baseline is frozen round11 with matched test fixtures, not round12 or the original user version. There are 52 existing caller/level cases, 15 public native-shaped cases and 20 complete-caller density controls: 87 cases, 522 samples per binary.

| Group | Significant reductions | Significant regressions | Inconclusive |
| --- | ---: | ---: | ---: |
| `CavlcLevelWriter` | 8 | 0 | 4 |
| `CavlcMacroblock` | 10 | 1 | 29 |
| `CavlcNativeFull` | 4 | 5 | 6 |
| `CavlcEmitterDensity` | 0 | 3 | 17 |
| Total | 22 | 9 | 56 |

The unweighted raw-median geomean is -1.92%. It is not a workload-weighted encoder speedup and does not override the failed controls. `Inconclusive` does not prove neutrality. P-values below are the recorded benchstat values, without a multiplicity correction; raw variance and all samples remain visible. There was no significance-seeking rerun or another cutoff search.

| Failed case | Change | p |
| --- | ---: | ---: |
| `CavlcMacroblock/InterAll/Zero` | +3.19% | 0.041 |
| `CavlcNativeFull/end3/zero/prefix17` | +4.84% | 0.041 |
| `CavlcNativeFull/end3/sparse/prefix17` | +4.33% | 0.037 |
| `CavlcNativeFull/end3/dense/prefix17` | +3.46% | 0.041 |
| `CavlcNativeFull/end3/mixed/prefix17` | +6.18% | 0.006 |
| `CavlcNativeFull/end14/zero/prefix17` | +7.01% | 0.002 |
| `CavlcEmitterDensity/InterAll/count6/Trailing3` | +7.80% | 0.004 |
| `CavlcEmitterDensity/InterAll/count8/Trailing3` | +7.53% | 0.002 |
| `CavlcEmitterDensity/I16All/count6/Trailing3` | +5.79% | 0.002 |

Dense/mixed reductions are retained in the complete tables, including end14/end15 mixed public writers and dense MB callers. The hold decision considers all cases. See [benchstat](benchstat.txt), [all 87 rows](all-87-cases.tsv), [root decision](decision.json), [independent audit](independent-audit.json), [base samples](base.txt) and [candidate samples](after.txt). Every original pair log is also present; [raw-samples.json](raw-samples.json) is a lossless extraction of the iteration counts and ns/op values.

## Correctness and provenance

The baseline full-encoder and native 447-suite evidence was reused only after exact source/log/native-binary verification. The baseline was rebuilt with the new density benchmark; all 20 fixtures executed once for setup sanity. The candidate received fresh full encoder/native 447-suite tests, exhaustive signed-domain and density-threshold tests, focused race tests and CPU-all-off tests. Actual native auto/scalar pass events were required, not merely successful process exit. All 50 recorded commands exited zero and their log hashes match. See [gate audit](archive-audit.json), [baseline reuse](baseline-validation-reuse.json), [candidate native validation](after-native447-validation.json) and [commands](commands.jsonl).

Correctness used the exact archived 447-suite coefficient fixture; timing used the exact prior 333-suite fixture and invoked no native binary. `CavlcNativeFull` names describe matched native-shaped fixtures, not native timing in this experiment. [provenance.json](provenance.json) and [timing-environment.json](timing-environment.json) record the settings and fixture hashes.

The frozen candidate inherited the first experiment's corrected unused-import test fix. [source-delta/tune-only](source-delta/tune-only/) records the five-file source-only tune against that validated first candidate. [source-delta/frozen](source-delta/frozen/) records the full candidate delta from round11; [source-delta/measured](source-delta/measured/) records the exact formatted baseline/candidate comparison. All source snapshots are `.txt` copies, so documentation does not introduce active Go packages. Shared correctness/benchmark fixtures and the diagnostic generator are included. Full build-input fingerprints are preserved separately.

The first held experiment remains unchanged in [round12](../../round12/cavlc-emitter-review/). [initial-evidence-references.json](initial-evidence-references.json) records its links and hashes. This experiment neither replaces its failed first-build log nor combines its timing samples with the follow-up.

## Archive contract and next-step boundary

Verbose full-test logs are deterministic gzip files. [archive-manifest.json](archive-manifest.json) records the stored and decoded SHA256 for every evidence file; [archive-files.sha256](archive-files.sha256) covers the final archive, excluding itself. Native/test binaries and any stream/profile payloads are omitted with size/hash records in [omitted-payloads.json](omitted-payloads.json). Coefficient fixtures are reproducible textual test inputs and are retained. The [native oracle source](native-oracle-source/) and its historical instructions are included as source references; the actual executable used is identified by the provenance hash and is not rebuilt here.

The [macroblock certificate design](design/cavlc-mb-certificate-review.md) is source review only. It includes native active-callsite evidence, safety constraints and an explicitly hypothetical CPU ceiling. The original note is preserved beside its portable-link copy. It is not implemented, measured or authorized as a third iteration by this archive.

This helper performs no builds, tests, benchmarks or benchstat reruns. It only verifies and archives completed evidence after the root's CPU queue is free. Round12 and all source/measurement trees are read-only inputs.
