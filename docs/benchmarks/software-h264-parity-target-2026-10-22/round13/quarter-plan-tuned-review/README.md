# Quarter-refinement fallback tune: held

Root held this candidate. Main remains the validated round12 implementation. This component experiment makes **no codec/full-frame, native-performance, stream-parity or live-RDP claim**. Main is not modified by this archive; the latest retained milestone at archival time is round12 (34 groups).

Root's explicit rationale: The single source-directed fallback tune improves18 of20 supported wide complete-callers, but fails the predeclared fallback controls. The4x4 stride32/motionfalse regression persists (+3.38%, p=.028), as does16x16 stride32/validated-off (+2.99%, p=.015); the16x16 stride32 scalar case also slows1.90% (p=.002). The7 unchanged public scorer controls are now inconclusive. Correctness, focused race, CPU-off, ARM64 build and zero-allocation gates pass, but this is not selected or merged. No more quarter tuning or timing repeat is planned in this round; no full-frame/native gain is claimed.

## Complete component result

The baseline is frozen **round11**, matching the initial held experiment. It is not round12 and the held initial candidate is not the timing baseline. The unchanged 47-case matrix covers 28 complete refinement callers (20 wide and eight narrow), seven public scorer controls and twelve wide scalar/nonbatch/validated-off caller controls. Six alternating pairs on CPU2, GOMAXPROCS=4 and 200ms per leaf produced 282 samples per binary / 564 total. Every sample reports zero bytes and allocations per operation.

| Case group | Significant reductions | Significant regressions | Inconclusive |
| --- | ---: | ---: | ---: |
| Wide complete callers | 18 | 0 | 2 |
| Narrow complete caller controls | 0 | 1 | 7 |
| Unchanged public scorer controls | 0 | 0 | 7 |
| Wide fallback caller controls | 0 | 2 | 10 |
| Total | 18 | 3 | 26 |

The unweighted raw-median geomean is -2.79%; it mixes paths and is not a frame-level gain. Inconclusive results do not prove neutrality. P-values are the recorded benchstat results without multiplicity correction. All wins and controls remain visible in [statistics](benchstat.txt), [all 47 cases](all-47-cases.md), [raw samples](samples.json), [base samples](base.txt), [candidate samples](after.txt), and [root decision](root-decision.json). The archive does not select candidates from p-values automatically.

## Change and validation scope

This one bounded tune checks width/current capabilities before entering the compact helper, and restores inline legacy descriptor construction for fallback. The original center callback and ordered MVD reads remain shared. The former legacy helper moved byte-for-byte into a test-only file. The existing four-candidate SATD assembly is unchanged. See [source review](harness/quarter-plan-tuned-next-README.txt), [full candidate delta](source-delta/vs-round11/changes.json), and [tune-only delta](source-delta/vs-held/changes.json).

Full encoder tests, focused race tests, full CPU-feature-disabled encoder tests, ARM64 cross-compilation and both allocation-test commands passed. Tests are component-scoped; there is no new full H264/AVC, native stream or decoder gate in this result. [Commands](commands.json), [provenance](provenance.json), [archive audit](archive-audit.json). The [saved codegen review](codegen-review.json) records a 768-byte parent frame versus 736 bytes in round11 and 416 bytes in the held initial version. The successful compact branch bypasses descriptor zeroing, and fallback calls the public scorer directly. This is evidence of generated call/frame structure, not a measurement of individual overhead costs.

The [initial held experiment](../quarter-plan-review/README.md) remains unchanged, including its narrow/validated-off regressions and unchanged public-control regression. Its source and selected evidence are guarded by this experiment, and its complete archived inventory is checked again before/after archival. No initial samples are combined with this follow-up. The [initial archive record](initial-archive-preservation.json) preserves those hashes.

## Archive and decision boundary

No full validation or merge is authorized by this held result. The prepared round13 full driver remains independent and unused for this candidate unless root makes a new explicit decision.

This helper writes only this fresh component archive. It never changes main, the Git index, parent milestone reports, the initial quarter archive, or existing round12 evidence. Binaries and duplicate full source trees are omitted; their hashes remain recorded. Exact changed source bodies have `.txt` suffixes. Compressed logs, if any, retain stored and decoded hashes in [archive-manifest.json](archive-manifest.json); [archive-files.sha256](archive-files.sha256) covers every final file except itself. This report is frozen at the component decision and does not describe later outcomes.
