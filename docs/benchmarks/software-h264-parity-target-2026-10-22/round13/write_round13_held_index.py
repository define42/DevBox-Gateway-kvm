#!/usr/bin/env python3
"""Prepared only. Root runs after all three held component archives exist.

Writes a held-experiments index and one parent link, preserving round12 as the
latest retained milestone. No builds/tests/benchmarks, selection, or source merge.
"""
from pathlib import Path
import hashlib
import json
import os
import re

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
DOCS = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22'
OUT = DOCS / 'round13'
PARENT = DOCS / 'README.md'
EXPECTED_PARENT = '681a6b2c7b47ddcfd58d3e0a1e62f1e2750a513c4db9bf16fadaed91046bcc88'
GROUPS = [('quarter-plan-review', 'decision.json', (16, 5, 26)),
          ('quarter-plan-tuned-review', 'root-decision.json', (18, 3, 26)),
          ('cavlc-emitter-tuned-review', 'decision.json', (22, 9, 56))]
LINK = 'The [round-thirteen held experiments](round13/README.md) preserve the rejected compact quarter-refinement attempts and tuned CAVLC emitter. They add no retained optimization group; round twelve remains the latest implementation milestone.\n\n'


def need(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def read_hashes(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        need(match is not None, 'Invalid source/inventory line')
        digest, name = match.groups()
        need(name not in result and not Path(name).is_absolute() and '..' not in Path(name).parts, 'Invalid inventory path')
        result[name] = digest
    return result


def atomic(path, text):
    temporary = path.with_name(path.name + '.prepared-tmp')
    need(not temporary.exists(), f'Unexpected temporary output: {temporary}')
    temporary.write_text(text)
    os.replace(temporary, path)


def main():
    outputs = ['README.md', 'held-index-audit.json', 'write_round13_held_index.py', 'archive-files.sha256']
    need(all(not (OUT / name).exists() for name in outputs), 'Fresh held index outputs required')
    need(sha(PARENT) == EXPECTED_PARENT, 'Parent report changed; review before editing')
    parent = PARENT.read_text()
    need('[latest report](round12/README.md)' in parent and 'thirty-four optimization groups' in parent, 'Expected latest round12/34 groups')
    anchor = 'These results describe the local Intel'
    need(parent.count(anchor) == 1 and '(round13/README.md)' not in parent, 'Unexpected parent-link context')
    before_main, before_index = tree(REPO / 'pkg/h264'), sha(REPO / '.git/index')
    round12 = {name.removeprefix('pkg/h264/'): digest for name, digest in
               read_hashes(ROOT / 'round12-review/current-sources.sha256').items() if name.startswith('pkg/h264/')}
    need(before_main == round12 and len(before_main) == 573, 'Main must still equal validated round12')
    preserved, decisions = {}, {}
    for name, decision_name, expected_counts in GROUPS:
        folder = OUT / name
        actual = tree(folder)
        need(read_hashes(folder / 'archive-files.sha256') == {k: v for k, v in actual.items() if k != 'archive-files.sha256'}, f'Archive inventory mismatch: {name}')
        decision = json.loads((folder / decision_name).read_text())
        need(decision['status'] == 'held' and decision['candidate_merged'] is False and
             decision['full_frame_or_native_timing_performed'] is False, f'Explicit held/pre-frame decision required: {name}')
        need(tuple(decision['counts'][key] for key in ['significant_reductions', 'significant_regressions', 'inconclusive']) == expected_counts, 'Outcome changed; review prose')
        preserved[name] = actual
        decisions[name] = {'path': str(folder / decision_name), 'sha256': sha(folder / decision_name), 'status': 'held', 'counts': decision['counts']}
    text = '''# Round thirteen: held experiments

All three component experiments below are **held**. Main remains the validated [round12 implementation](../round12/README.md): 34 retained optimization groups and 573 H264 files. Native speed parity has not been achieved. This index adds no retained implementation milestone.

| Experiment | Significant reductions | Significant regressions | Inconclusive | Decision |
| --- | ---: | ---: | ---: | --- |
| [Initial compact quarter-refinement plan](quarter-plan-review/README.md) | 16 | 5 | 26 | Held for narrow/fallback caller regressions; an unchanged public control also regressed. |
| [Quarter-refinement fallback tune](quarter-plan-tuned-review/README.md) | 18 | 3 | 26 | Held because narrow and wide fallback controls still regress. |
| [Density-gated CAVLC emitter tune](cavlc-emitter-tuned-review/README.md) | 22 | 9 | 56 | Held for sparse/zero and complete-macroblock control regressions. |

Each quarter experiment used the same 47-case matrix and six alternating baseline/candidate pairs against frozen round11. The first candidate's twenty wide complete callers included sixteen significant reductions, but its controls failed. The one bounded tune restored inline fallback construction and cheap eligibility checks. It produced eighteen wide caller reductions, yet the 4×4/stride32/no-motion control regressed 3.38% (p=.028), the 16×16/stride32/validated-off control regressed 2.99% (p=.015), and the scalar 16×16/stride32 control regressed 1.90% (p=.002). All seven unchanged public scorer controls were inconclusive in the tune. Both attempts and every sample remain separate; their samples are not pooled. [Tuned statistics](quarter-plan-tuned-review/benchstat.txt), [root decision](quarter-plan-tuned-review/root-decision.json).

The CAVLC tune used 87 cases and six alternating pairs against frozen round11. Dense/mixed wins did not clear the failed zero and density controls. Its first attempt remains preserved in [round12](../round12/cavlc-emitter-review/decision.txt). [Tuned statistics](cavlc-emitter-tuned-review/benchstat.txt), [root decision](cavlc-emitter-tuned-review/decision.json).

All candidates passed their recorded component correctness gates. Quarter validation includes encoder/race/CPU-disabled/ARM64/allocation checks; CAVLC includes the recorded native coefficient-parity suite. These scopes differ and are documented in their reports. None of these component outcomes ran the full round13 codec/EncodeDamage/native timing and 960-frame stream/decoder gate. No frame-level improvement, stream-parity extension, native speed parity or live RDP acceleration is established by this index. The prepared round13 full runner was not used because no candidate cleared its component gate.

The tables report all recorded significance results without multiplicity correction. Inconclusive does not prove neutral; unweighted component geomeans are not workload-level speedups. No additional cutoff search, fallback tuning iteration or significance-seeking repeat follows these decisions.

The [index audit](held-index-audit.json) records source/index preservation and the exact component-archive inventories. [Archive file hashes](archive-files.sha256) cover this index and the nested evidence. The parent report continues to identify round12 as the latest retained milestone.
'''
    new_parent = parent.replace(anchor, LINK + anchor)
    need(new_parent.replace(LINK, '', 1) == parent, 'Parent change must be exactly the held-evidence link')
    for name, mapping in preserved.items():
        need(tree(OUT / name) == mapping, 'Component archive changed during index preparation')
    need(tree(REPO / 'pkg/h264') == before_main and sha(REPO / '.git/index') == before_index, 'Source or index changed')
    atomic(OUT / 'README.md', text)
    atomic(OUT / 'write_round13_held_index.py', Path(__file__).read_text())
    atomic(PARENT, new_parent)
    audit = {'status': 'all_candidates_held', 'latest_retained_milestone': 'round12', 'retained_groups': 34,
             'h264_files': 573, 'main_h264_unchanged': tree(REPO / 'pkg/h264') == before_main,
             'git_index_unchanged': sha(REPO / '.git/index') == before_index,
             'parent_before_sha256': EXPECTED_PARENT, 'parent_after_sha256': sha(PARENT),
             'parent_change': 'One held-experiment link only; latest milestone/group count unchanged',
             'component_decisions': decisions, 'preserved_component_archives': preserved,
             'full_round13_runner_used': False, 'native_codec_frame_timing_added': False,
             'source_merge': False}
    need(audit['main_h264_unchanged'] and audit['git_index_unchanged'], 'Source/index changed during write')
    atomic(OUT / 'held-index-audit.json', json.dumps(audit, indent=2) + '\n')
    inventory = tree(OUT)
    atomic(OUT / 'archive-files.sha256', ''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(OUT / 'README.md')


if __name__ == '__main__':
    main()
