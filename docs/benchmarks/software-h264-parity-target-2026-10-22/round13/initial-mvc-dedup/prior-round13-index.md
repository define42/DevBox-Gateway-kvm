# Round thirteen: held experiments

All three component experiments below are **held**. Main remains the validated [round12 implementation](../round12/README.md): 34 retained optimization groups and 573 H264 files. Native speed parity has not been achieved. This index adds no retained implementation milestone.

| Experiment | Significant reductions | Significant regressions | Inconclusive | Decision |
| --- | ---: | ---: | ---: | --- |
| [Initial compact quarter-refinement plan](quarter-plan-review/README.md) | 16 | 5 | 26 | Held for narrow/fallback caller regressions; an unchanged public control also regressed. |
| [Quarter-refinement fallback tune](quarter-plan-tuned-review/README.md) | 18 | 3 | 26 | Held because narrow and wide fallback controls still regress. |
| [Density-gated CAVLC emitter tune](cavlc-emitter-tuned-review/README.md) | 22 | 9 | 56 | Held for sparse/zero and complete-macroblock control regressions. |

Each quarter experiment used the same 47-case matrix and six alternating baseline/candidate pairs against frozen round11. The first candidate's twenty wide complete callers included sixteen significant reductions, but its controls failed. The one bounded tune restored inline fallback construction and cheap eligibility checks. It produced eighteen wide caller reductions, yet the 4×4/stride32/no-motion control regressed 3.38% (p=.028), the 16×16/stride32/validated-off control regressed 2.99% (p=.015), and the scalar 16×16/stride32 control regressed 1.90% (p=.002). All seven unchanged public scorer controls were inconclusive in the tune. Both attempts and every sample remain separate; their samples are not pooled. [Tuned statistics](quarter-plan-tuned-review/benchstat.txt), [root decision](quarter-plan-tuned-review/root-decision.json).

The CAVLC tune used 87 cases and six alternating pairs against frozen round11. Dense/mixed wins did not clear the failed zero and density controls. Its first attempt remains preserved in [round12](../round12/cavlc-emitter-review/decision.txt). [Tuned statistics](cavlc-emitter-tuned-review/benchstat.txt), [root decision](cavlc-emitter-tuned-review/decision.json).

All candidates passed their recorded component correctness gates. Quarter validation includes encoder/race/CPU-disabled/ARM64/allocation checks; CAVLC includes the recorded native coefficient-parity suite. These scopes differ and are documented in their reports. None of these component outcomes ran the full round13 codec/EncodeDamage/native timing and 960-frame stream/decoder gate. No frame-level improvement, stream-parity extension, native speed parity or live RDP acceleration is established by these component experiments. The prepared round13 full runner was not used because no candidate cleared its component gate.

The tables report all recorded significance results without multiplicity correction. Inconclusive does not prove neutral; unweighted component geomeans are not workload-level speedups. No additional cutoff search, fallback tuning iteration or significance-seeking repeat follows these decisions.

<!-- bswrite-flush-precheck:start -->
## Pre-timing hold: common bit-writer flush extraction

A separate [common `BsWriteBits` compiler precheck](bswrite-flush-precheck/README.md) stopped before performance measurement. The two straightforward helper forms cost 83 and 84 against the compiler's inline budget of 80; neither made the writer inlineable. Their correctness tests and benchmark fixtures remain unfinished, unrun drafts. This adds no component timing, retained change, or frame-level claim. The earlier measured component experiments remain held.
<!-- bswrite-flush-precheck:end -->

## Diagnostic only: initial motion candidates

A separate [initial-MVC counting diagnostic](initial-mvc-diagnostic/README.md) records distinct clipped candidates on Road and PDF using an isolated round11 overlay. Its complete streams match the uninstrumented round11 streams across 616 recorded frames each. It changes no production source and is not a new optimization, native comparison or performance measurement. All instrumented timing lines are excluded from performance evidence. The verified counts may inform a separate proposal; the three component decisions above remain held.

The [index audit](held-index-audit.json) records source/index preservation and the exact component-archive inventories. [Archive file hashes](archive-files.sha256) cover this index and the nested evidence. The parent report continues to identify round12 as the latest retained milestone.
