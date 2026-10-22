# Common bit-writer flush extraction: held before timing

This source experiment stopped at its compiler gate. Both straightforward forms preserve the original pointer-based word-store sequence but fail to make `BsWriteBits` inlineable. Main was not changed, and no speedup is claimed.

| Form | Reported inline cost | Budget | Result |
| --- | ---: | ---: | --- |
| Original fast branch plus void flush helper | 83 | 80 | Not inlineable |
| Inverse branch returning the flush helper's result | 84 | 80 | Not inlineable |

The two root-run `go build -gcflags=...=-m=2` commands exited successfully; their compiler diagnostics reject inlineability. These builds did not execute tests or benchmarks. Exact reported commands, explicit environment overrides, unrecorded inherited-environment scope, and source hashes are in [root command provenance](root-reported-commands.json). The archive helper does not rerun them.

[First source](frozen-evidence/first-pointer-void.go.txt), [second source](frozen-evidence/second-pointer-return.go.txt), and the [frozen round12 baseline](frozen-evidence/baseline.go.txt) are preserved verbatim. Both [first log](frozen-evidence/bswrite-flush-inline-precheck.txt) and [second log](frozen-evidence/bswrite-flush-inline-return-precheck.txt) retain all diagnostics; root's before/after source-hash records are copied separately. The [source manifest](frozen-evidence/manifest.json) and [original handoff](source-review-README.txt) identify the complete source scope.

The four reference/test/benchmark drafts under `frozen-evidence/unrun-drafts/` are unfinished and **unvalidated**. They were not compiled as tests or executed, and complete macroblock-syntax benchmark fixtures were not finished after the compiler gate failed. The common bit writer is distinct from the held CAVLC emitter; no CAVLC production source changed.

This is a pre-timing hold, separate from the earlier measured component experiments in [round13](../README.md), which remain held. No component timing, stream-parity extension, native comparison, frame timing, or live RDP acceleration follows from this check. [Decision](decision.json), [preservation audit](archive-audit.json), [file hashes](archive-files.sha256).
