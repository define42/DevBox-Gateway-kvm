# Initial motion-candidate diagnostic

This is an isolated **counting diagnostic**, not a retained optimization or a performance measurement. Main remains validated round12 (34 groups, 573 H264 files). No candidate is selected by this archive.

A three-file overlay on frozen round11 observes the initial MVP plus five clipped motion-vector candidates for 16×16 blocks. It preserves the existing SAD calls and decision order. It records distinct vectors and conservative eligibility for a possible future built-in batch; it does not implement that batch. Native also evaluates these candidates separately, so this is not evidence of a missing native six-candidate kernel. [Source review](remaining-motion-review.txt), [overlay source](overlay/), [diagnostic instructions](diagnostic-README.txt).

## Parity and scope

Road and PDF each encoded 600 requested frames plus 16 warmup frames, at 1920×1080, QP20, one layer/thread/slice, pinned to CPU2. Each complete output stream's SHA256 matches the frozen uninstrumented round11 stream: **616 frames per case, 1,232 recorded frames total**. The archive verifies the complete stream bytes by hash and the recorded frame lifecycle; it does not run a new decoder or compare against native streams. The giant streams and test binary are omitted with exact size/hash records. [Parity/payload records](omitted-payloads.json), [completed summary](summary.json), [provenance](provenance.json).

Every timing line printed by this instrumented binary is unsuitable for performance comparisons. Logging, counting and stream capture change execution cost. No benchmark statistics, encoder speedup or native speed ratio is derived here. The full round13 codec/EncodeDamage/native timing gate was not invoked for this diagnostic.

## Counted work

| Fixture | Eligible searches | Mean unique vectors | Prior SAD evaluations | Unique vector evaluations | Potential repeated evaluations (share of prior calls) | Conservative preflight-eligible searches |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| road1 | 3,770,737 | 4.309 | 18,278,239 | 16,247,022 | 2,031,217 (11.11%) | 3,770,737 |
| pdf1 | 490,448 | 2.878 | 1,953,073 | 1,411,407 | 541,666 (27.73%) | 490,448 |

The prior SAD total includes the initial evaluation and only the extra calls surviving the original current-best-vector skip. Unique-vector totals deduplicate all six clipped positions in first-occurrence order. Their difference counts potentially repeated evaluation work for immutable built-in inputs; it is not time saved. The entry-snapshot histogram includes excluded/custom paths; only the separate preflight histogram describes conservative eligibility for a possible pure built-in route.

| Fixture | 1 unique | 2 unique | 3 unique | 4 unique | 5 unique | 6 unique |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| road1 | 96,435 | 161,736 | 536,950 | 1,164,863 | 1,307,705 | 503,048 |
| pdf1 | 93,246 | 69,177 | 178,084 | 117,755 | 18,581 | 13,605 |

All histogram, duplicate and exclusion identities are checked, including the preflight subset. Prior initial-search and five-MVC totals must match exactly. [Road counters](road1-counts.json), [PDF counters](pdf1-counts.json), [prior counter evidence](reference-counts/), [archive audit](archive-audit.json).

These counts may inform a separately authorized, correctness-tested and benchmarked implementation. The entire InitialPoint path was only 6.28% of Road and 1.28% of PDF CPU in the cited earlier profile; this subset's possible gain is smaller, and dispatch/deduplication/preflight costs may erase it. Those historical cumulative shares are ceilings, not measured savings from this diagnostic.

The archive preserves all three earlier held component archives exactly and leaves main and the Git index unchanged. [File hashes](archive-files.sha256) cover every final artifact except the inventory itself; [archive manifest](archive-manifest.json) identifies copied/generated evidence. Source is stored as `.go.txt` references and must not be merged.
