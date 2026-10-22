HELD: common BsWriteBits slow-path extraction (source-only experiment)

Baseline: /tmp/h264-parity-target-20261022/round12-next
Isolated tree: /tmp/h264-parity-target-20261022/bswrite-flush-next
Evidence: /tmp/h264-parity-target-20261022/bswrite-flush-source-review
Main repository: unchanged by this experiment.

Decision
--------
Stop at the compiler gate. The first source form keeps the original fast branch
and invokes a private noinline pointer helper on flush. Root's compiler precheck
reported cost83, above80. The second form makes the helper return int32 and uses
an early return from the inverse branch; the compiler reported cost84, above80.
Neither makes BsWriteBits inlineable. No timing or performance improvement is
claimed. No third compiler trick, state reshaping, or timing iteration is proposed.
The independent source reviewer found the production field/store/panic order
preserved; this does not replace correctness execution.

Exact production source hashes
------------------------------
first-pointer-void.go.txt:
e0b54ea296744d5631a01c255d03be83d8c95807bf168ad7f9862edb6923881f
second-pointer-return.go.txt (current isolated production file):
7cb43cc039dce9269a7f601a88e47c617309667fef38d2702fc1ae1249551f76
Both exactly match the source hashes recorded by root before its corresponding
compiler check. Original logs are copied unchanged under the evidence directory.
Production delta is only pkg/h264/internal/common/golomb_common_h.go.
No CAVLC writer changes and no assembly changes.

Validation scope
----------------
Root executed two compiler prechecks successfully; they rejected inlineability.
No common or encoder correctness test, race/CPU-off test, benchmark, native
comparison, stream check, or frame timing was executed for this candidate.
No Go command was run by the source author.

Unfinished, unexecuted drafts retained for provenance
---------------------------------------------------
pkg/h264/internal/common/bswrite_flush_reference_test.go
pkg/h264/internal/common/bswrite_flush_test.go
pkg/h264/internal/common/bswrite_flush_bench_test.go
pkg/h264/internal/encoder/bswrite_syntax_reference_test.go

The common reference is the frozen round12 writer renamed only. Draft common
tests cover pending1..32 and malformed states, signed/unsigned length/value
boundaries, nil/short buffers and partial byte stores, scalar-state aliases,
UE/SE controls and mutable Golomb table aliases. They remain UNRUN and should
not be treated as a passing test suite. The encoder frozen reference was written,
but complete macroblock fixtures were intentionally not expanded after the gate
failed. Copies use .go.txt inside the evidence folder to avoid accidental builds.

Draft standalone public common benchmark cases (baseline-compatible file)
-----------------------------------------------------------------------
BenchmarkBsWriteBitsPublic: Fast1, Fast3, Fast17, FlushEqual, FlushSpill, Flush32,
ZeroLength, NegativeLength, ZeroLeft (9 cases).
BenchmarkBsWriteUEPublic:0,1,31,255,256,65535 with left1/32 (12 cases).
BenchmarkBsWriteSEPublic:0,1,-1,32,-32,32767,-32768 with left1/32 (14 cases).
These35 cases were not measured. No complete-MB benchmark fixture was frozen.

Evidence manifest
-----------------
bswrite-flush-source-review/manifest.json records baseline/candidate paths,
all draft/source hashes, both compiler logs, and the no-validation limitation.
Its SHA256 is13d4a4df0fa18cd715a7f4ab5522e22461217b924b7ede6412daffff008f8581.
