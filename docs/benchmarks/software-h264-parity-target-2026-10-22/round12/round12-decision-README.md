# Decision-aware round twelve evidence

These are separate source-only helpers. The frozen `save_round12.py` and `write_round12_report.py` remain unchanged. No selection is assumed. The archive/report helpers have not been executed by their author.

After the single predeclared confirmation completes, run `verify_round12_confirmation_inputs.py`. It records a post-run-only comparison with validated first-pass input hashes, without claiming any pre/mid-confirmation check.

Root then writes `/tmp/h264-parity-target-20261022/round12-decision.json` with an explicit decision:

```json
{"status": "held", "rationale": "Root's evidence-based selection explanation."}
```

The only accepted status values are `held` and `retained`. Additional decision metadata is retained unchanged. This final decision is distinct from the older provisional `round12-selection.json`.

If retained, first run `merge_round12_main.py`; if held, do not run a merge. Then run:

```sh
python3 /tmp/h264-parity-target-20261022/save_round12_decision.py
python3 /tmp/h264-parity-target-20261022/write_round12_decision_report.py
```

The archival wrapper requires complete first-pass/confirmation runs and matching post-run input verification. For retained source it calls the frozen archiver if needed, or extends an existing retained archive. For held source it uses `save_round12_held.py`: main must remain byte-identical to round eleven, and an eight-file candidate patch is replayed against round eleven to reproduce the measured 573-file snapshot. The held path keeps 33 retained groups and the parent index's latest main milestone at round eleven. No index changes occur.

Both outcomes archive every confirmation sample, provenance, explicit decision and verification, while preserving original first-pass and component evidence. The existing scratch archive remains byte-identical. The held initial CAVLC failure, repair, frozen/working source hashes and six paired timing results are preserved; its two verbose logs are gzip-compressed with decompression/hash metadata. Binaries and streams remain omitted with hashes.

The decision-aware report adds the complete five-case longer confirmation without pooling it with the first 80-frame pass or hiding the original ShortClip regression. Full-path incremental and cumulative comparisons remain distinct. An existing retained first-pass report can be extended; a held report clearly labels all candidate measurements and never promotes them to current main performance.

All four new helpers have only been AST-parsed. They have not run tests, builds, benchmarks, fixture hashing, patch replay, compression, production merges or documentation writes during source preparation.

Post-run verification was executed after the confirmation completion marker existed: 2026-10-22T19:29:38.538249+00:00. All five configured cases (4 distinct files) matched the validated first-pass hashes. See `round12-single-confirmation/post-run-input-verification.json`. No archive/report/merge helper was executed.
