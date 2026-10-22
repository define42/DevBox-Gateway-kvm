#!/usr/bin/env python3
"""Archive the held initial quarter-plan experiment without changing main."""
from pathlib import Path
import difflib
import hashlib
import json
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
RUN = ROOT / 'quarter-plan-review'
OUT = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round13/quarter-plan-review'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def load(path):
    return json.loads(path.read_text())


def main():
    summary, decision = load(RUN / 'summary.json'), load(RUN / 'decision.json')
    assert summary['status'] == 'complete' and summary['total_samples'] == 564
    assert summary['all_samples_zero_allocations'] and decision['status'] == 'held'
    assert decision['counts'] == {'significant_reductions': 16, 'significant_regressions': 5, 'inconclusive': 26}
    assert all(c['exit_code'] == 0 for c in load(RUN / 'commands.json'))
    before, after = ROOT / 'round11', ROOT / 'quarter-plan-next'
    changes = load(RUN / 'changes-vs-round11.json')
    assert len(changes) == 8
    main_before, index_before = tree(REPO / 'pkg/h264'), sha(REPO / '.git/index')
    assert not OUT.exists()
    stage = Path(tempfile.mkdtemp(prefix='quarter-plan-archive-', dir=ROOT))
    records, omitted = [], []
    for source in sorted(RUN.rglob('*')):
        if not source.is_file():
            continue
        rel = source.relative_to(RUN)
        if rel.parts[0] == 'work':
            continue
        if rel.parts[0] == 'bin':
            omitted.append({'source': str(source), 'sha256': sha(source), 'bytes': source.stat().st_size})
            continue
        target = stage / rel
        if target.suffix == '.go':
            target = target.with_name(target.name + '.txt')
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
        assert sha(target) == sha(source)
        records.append({'source': str(source), 'archive': str(target.relative_to(stage)), 'sha256': sha(target)})
    patch = []
    for entry in changes:
        rel = entry['path']
        assert rel.startswith('pkg/h264/')
        old, new = before / rel, after / rel
        assert (sha(old) if old.exists() else None) == entry['before_sha256']
        assert sha(new) == entry['after_sha256']
        old_lines = old.read_text().splitlines(keepends=True) if old.exists() else []
        new_lines = new.read_text().splitlines(keepends=True)
        assert all(not lines or lines[-1].endswith('\n') for lines in [old_lines, new_lines])
        patch.extend(difflib.unified_diff(old_lines, new_lines,
                     fromfile='a/' + rel if old.exists() else '/dev/null', tofile='b/' + rel))
        for kind, source in [('before', old), ('after', new)]:
            if source.exists():
                target = stage / 'source-delta' / kind / (rel + '.txt')
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, target)
    patch_path = stage / 'candidate-vs-round11.patch'
    patch_path.write_text(''.join(patch))
    with tempfile.TemporaryDirectory(prefix='quarter-plan-replay-', dir=ROOT) as temporary:
        replay = Path(temporary)
        shutil.copytree(before / 'pkg/h264', replay / 'pkg/h264')
        subprocess.run(['git', 'apply', '--check', '--whitespace=error-all', str(patch_path)], cwd=replay, check=True)
        subprocess.run(['git', 'apply', str(patch_path)], cwd=replay, check=True)
        assert tree(replay / 'pkg/h264') == tree(after / 'pkg/h264')
    shutil.copy2(Path(__file__), stage / Path(__file__).name)
    shutil.copy2(ROOT / 'quarter-plan-next-README.txt', stage / 'candidate-source-review.txt')
    (stage / 'README.md').write_text('''# Initial compact quarter-refinement plan: held

The candidate passed correctness checks and improved sixteen of twenty wide-block complete refinement callers by 3.67–11.72%. It remains **held** because two narrow callers and two wide validated-off callers regressed. One unchanged public scorer control also regressed, so code placement and timing variation limit attribution. Main is unchanged by this experiment.

The complete matrix contains 47 cases: 28 full callers (including eight narrow fallback cases), seven unchanged public scorer controls and twelve additional wide scalar/nonbatch/validated-off callers. Six alternating pairs on CPU2 produced 564 samples, all with zero allocations. There are 16 significant reductions, five significant regressions and 26 inconclusive results. The combined geomean mixes paths and is not a frame-level gain. [Statistics](benchstat.txt), [samples](samples.json), [decision](decision.json).

Full encoder tests, focused race tests, full CPU-feature-disabled tests, an ARM64 cross-build and allocation tests passed. This experiment did not run native timing, codec/full-frame timing or stream/decoder comparisons. The new source uses the existing unchanged four-candidate SATD kernel; it reduces temporary construction and repeated span validation, rather than adding a native algorithm. [Commands](commands.json), [scope](provenance.json), [source review](candidate-source-review.txt).

The measured parent frame shrank from 736 to 416 bytes, but the fallback gained calls to a 784-byte compact helper and a separate 376-byte legacy constructor. These are reserved stack sizes, not measured CPU costs. The saved assembly establishes that the extra call boundaries exist. A single separate tune is allowed to restore inline fallback construction and bypass the compact helper when ineligible; it must rerun this entire matrix. This initial result remains frozen.

The eight-file [candidate patch](candidate-vs-round11.patch) replays on frozen round eleven to reproduce every candidate H264 file. The identical benchmark-only fallback overlay is in [harness](harness/quarter_plan_fallback_bench_amd64_test.go.txt); it was copied into both measured trees and is not part of that production candidate patch. Source fingerprints distinguish frozen and measured trees. [Archive audit](archive-audit.json), [file hashes](archive-files.sha256). Binaries and duplicate full source trees are omitted; their hashes and the changed source bodies are retained. No Git index changes occur.
''')
    audit = {'status': 'held', 'copied_artifacts': records, 'omitted_binaries': omitted,
             'candidate_patch_replayed': True, 'candidate_files': len(tree(after / 'pkg/h264')),
             'main_h264_unchanged': tree(REPO / 'pkg/h264') == main_before,
             'git_index_unchanged': sha(REPO / '.git/index') == index_before,
             'counts': decision['counts'], 'sample_count': summary['total_samples']}
    assert audit['main_h264_unchanged'] and audit['git_index_unchanged']
    (stage / 'archive-audit.json').write_text(json.dumps(audit, indent=2) + '\n')
    inventory = tree(stage)
    (stage / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    OUT.parent.mkdir(parents=True, exist_ok=True)
    shutil.move(str(stage), OUT)
    print(OUT)


if __name__ == '__main__':
    main()
