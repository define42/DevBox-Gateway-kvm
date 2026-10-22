#!/usr/bin/env python3
"""Archive an explicit round12 retain/hold decision and the bounded confirmation.

Run after root writes round12-decision.json. If retained, merge first. Existing
frozen successful-merge helpers and the first-pass evidence are not modified.
"""
from pathlib import Path
import hashlib
import json
import shutil

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
DEST = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round12'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def load(path):
    return json.loads(path.read_text())


def main():
    decision = load(ROOT / 'round12-decision.json')
    assert decision['status'] in {'retained', 'held'} and decision['rationale'].strip()
    confirmation = ROOT / 'round12-single-confirmation'
    complete = load(confirmation / 'complete.json')
    assert complete['complete'] and complete['cases'] == 5 and complete['pairs_per_case'] == 6
    verification = load(confirmation / 'post-run-input-verification.json')
    assert verification['all_match'] and verification['case_count'] == 5
    assert 'Post-run' in verification['scope']
    metadata = load(confirmation / 'provenance.json')
    assert metadata['pairs'] == 6 and metadata['measured_frames'] == 240 and metadata['warmup_frames'] == 16 and metadata['native_reruns'] == 0
    assert metadata['binaries'] == {kind: load(ROOT / 'round12-review/binary-hashes.json')[kind] for kind in ['round11', 'current']}
    for path, digest in metadata['prior_artifact_sha256'].items():
        assert sha(Path(path)) == digest, f'First-pass evidence changed: {path}'
    scratch = DEST / 'scratch-isolation-review'
    assert scratch.is_dir()
    preserved_scratch = tree(scratch)
    frozen_helpers = {name: sha(ROOT / name) for name in ['save_round12.py', 'write_round12_report.py']}
    if not (DEST / 'archive-manifest.json').exists():
        if decision['status'] == 'retained':
            import save_round12
            save_round12.main()
        else:
            import save_round12_held
            save_round12_held.main()
    archive = load(DEST / 'archive-manifest.json')
    if decision['status'] == 'retained':
        assert archive['retained_groups'] == 34 and archive['main_equals_measured_snapshot']
    else:
        assert archive['retained_groups'] == 33 and archive['main_equals_round11'] and not archive['candidate_merged']
    assert not (DEST / 'confirmation').exists() and not (DEST / 'decision.json').exists(), 'Decision evidence already archived; refuse replacement'
    run_before = tree(DEST / 'run')
    initial_components = {folder: tree(DEST / folder) for folder in ['intra4-directional-review', 'cavlc-emitter-review']}
    for path in sorted(confirmation.iterdir()):
        assert path.is_file() and path.suffix in {'.txt', '.json', '.log'}, path
    shutil.copytree(confirmation, DEST / 'confirmation')
    shutil.copy2(ROOT / 'round12-decision.json', DEST / 'decision.json')
    extras = ['confirm_round12_single_slice.py', 'verify_round12_confirmation_inputs.py',
              'save_round12_decision.py', 'save_round12_held.py', 'write_round12_decision_report.py',
              'round12-decision-README.md', 'round12-selection.json']
    for name in extras:
        source, target = ROOT / name, DEST / name
        if target.exists():
            assert sha(source) == sha(target), f'Archived helper changed: {name}'
        else:
            shutil.copy2(source, target)
    assert tree(DEST / 'run') == run_before
    assert all(tree(DEST / folder) == hashes for folder, hashes in initial_components.items())
    assert tree(scratch) == preserved_scratch
    assert all(sha(ROOT / name) == digest for name, digest in frozen_helpers.items())
    audit = {'decision': decision, 'retained_groups': archive['retained_groups'],
             'confirmation_complete': complete, 'post_run_input_verification': verification,
             'first_pass_evidence_preserved': True, 'initial_component_evidence_preserved': True,
             'scratch_archive_preserved': True, 'original_helpers_unchanged': frozen_helpers,
             'confirmation_archived_files': tree(DEST / 'confirmation'),
             'selection_manifest_is_provisional': 'round12-selection.json predates the explicit final decision.json'}
    (DEST / 'decision-archive-audit.json').write_text(json.dumps(audit, indent=2) + '\n')
    inventory = tree(DEST)
    inventory.pop('archive-files.sha256', None)
    (DEST / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(DEST)


if __name__ == '__main__':
    main()
