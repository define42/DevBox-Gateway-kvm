#!/usr/bin/env python3
"""Prepared source only: root may execute once, sequentially with other archives.

No compiler/test/benchmark command is executed. Only the new archive, one marked
round13 README section, and the current round13 inventory may be written.
"""
from datetime import datetime, timezone
from pathlib import Path
import hashlib
import json
import re
import shutil

R = Path('/tmp/h264-parity-target-20261022')
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
DOCS = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22'
ROUND = DOCS / 'round13'
SRC = R / 'bswrite-flush-source-review'
DEST = ROUND / 'bswrite-flush-precheck'
STAGE = ROUND / 'bswrite-flush-precheck.incomplete'
README = R / 'bswrite-flush-next-README.txt'
REL = 'pkg/h264/internal/common/golomb_common_h.go'
MANIFEST_SHA = '13d4a4df0fa18cd715a7f4ab5522e22461217b924b7ede6412daffff008f8581'
README_SHA = 'cdad4e497e05eb6f775aa15acd974d425864c4c19d428bb8d5d3d9f48bc19d57'
VARIANTS = [
    ('first-pointer-void.go.txt', 'bswrite-flush-inline-precheck', 83,
     'e0b54ea296744d5631a01c255d03be83d8c95807bf168ad7f9862edb6923881f'),
    ('second-pointer-return.go.txt', 'bswrite-flush-inline-return-precheck', 84,
     '7cb43cc039dce9269a7f601a88e47c617309667fef38d2702fc1ae1249551f76'),
]
MARKER = '<!-- bswrite-flush-precheck:start -->'
SECTION = '''<!-- bswrite-flush-precheck:start -->
## Pre-timing hold: common bit-writer flush extraction

A separate [common `BsWriteBits` compiler precheck](bswrite-flush-precheck/README.md) stopped before performance measurement. The two straightforward helper forms cost 83 and 84 against the compiler's inline budget of 80; neither made the writer inlineable. Their correctness tests and benchmark fixtures remain unfinished, unrun drafts. This adds no component timing, retained change, or frame-level claim. The earlier measured component experiments remain held.
<!-- bswrite-flush-precheck:end -->

'''


def need(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def hashes(tree, omit=()):
    return {str(p.relative_to(tree)): sha(p) for p in sorted(tree.rglob('*'))
            if p.is_file() and str(p.relative_to(tree)) not in omit}


def inventory(tree):
    return ''.join(f'{digest}  {name}\n' for name, digest in
                   hashes(tree, ('archive-files.sha256',)).items())


def parse_inventory(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        need(match is not None, f'bad inventory line in {path}')
        name = match[2]
        need(not Path(name).is_absolute() and '..' not in Path(name).parts and name not in result,
             f'unsafe/duplicate inventory path {name}')
        result[name] = match[1]
    return result


def put_json(name, data):
    (STAGE / name).write_text(json.dumps(data, indent=2, sort_keys=True) + '\n')


def main():
    need(not DEST.exists() and not STAGE.exists(), 'archive/stage already exists; do not overwrite')
    need(sha(SRC / 'manifest.json') == MANIFEST_SHA, 'frozen source manifest changed')
    need(sha(README) == README_SHA, 'frozen source README changed')
    manifest = json.loads((SRC / 'manifest.json').read_text())
    for name, digest in manifest['archive_sha256'].items():
        path = SRC / name
        need(path.is_file() and not path.is_symlink() and sha(path) == digest,
             f'frozen evidence changed: {name}')
    need(set(hashes(SRC)) == set(manifest['archive_sha256']) | {'manifest.json'},
         'unexpected source evidence files')
    need(sha(R / 'round12-next' / REL) == sha(SRC / 'baseline.go.txt'), 'baseline source changed')
    commands = []
    for source, stem, cost, digest in VARIANTS:
        need(sha(SRC / source) == digest, f'wrong production variant {source}')
        record = R / (stem + '-source.sha256')
        need(record.read_text() == f'{digest}  {REL}\n', f'root source-hash record changed: {stem}')
        log = R / (stem + '.txt')
        need(sha(log) == sha(SRC / log.name), f'root compiler log changed: {stem}')
        text = log.read_text()
        observed = re.findall(r'cannot inline BsWriteBits: function too complex: cost (\d+) exceeds budget (\d+)', text)
        need(observed == [(str(cost), '80')], f'inline diagnostic differs: {stem}')
        need('cannot inline bsWriteBitsFlush: marked go:noinline' in text, 'missing helper diagnostic')
        commands.append({
            'name': stem, 'evidence_kind': 'root-reported completed command, not a replay',
            'argv': ['/snap/go/current/bin/go', 'build',
                     '-gcflags=github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common=-m=2',
                     './pkg/h264/internal/common'],
            'cwd': str(R / 'bswrite-flush-next'),
            'environment_explicit_overrides': {'CGO_ENABLED': '0', 'GOMAXPROCS': '4',
                'GOWORK': 'off', 'GOARCH': 'amd64', 'GOOS': 'linux', 'GOAMD64': 'v1'},
            'other_environment': 'os.environ.copy(); other inherited variables were not recorded/sanitized',
            'capture': 'subprocess text=True, stdout=PIPE, stderr=STDOUT; full output written to log',
            'returncode_reported_by_root': 0, 'source_sha256_before_and_after_reported_by_root': digest,
            'source_record': 'root-source-records/' + record.name,
            'source_record_sha256': sha(record),
            'log': 'frozen-evidence/' + log.name, 'log_sha256': sha(log),
            'provenance': 'Parent-agent messages supplying exact argv/environment and confirming exit0 and source assertions; no saved original commands JSON existed.'})
    for decision_path in ['quarter-plan-review/decision.json',
                          'quarter-plan-tuned-review/root-decision.json',
                          'cavlc-emitter-tuned-review/decision.json']:
        need(json.loads((ROUND / decision_path).read_text())['status'] == 'held',
             f'prior component decision changed: {decision_path}')
    old = hashes(ROUND)
    need(parse_inventory(ROUND / 'archive-files.sha256') == hashes(ROUND, ('archive-files.sha256',)),
         'current round13 inventory is stale; run archive helpers sequentially')
    index_before = (ROUND / 'README.md').read_bytes()
    need(MARKER.encode() not in index_before, 'precheck section already present')
    protected = {str(DOCS / 'README.md'): sha(DOCS / 'README.md'),
                 str(REPO / '.git/index'): sha(REPO / '.git/index')}
    main_before = hashes(REPO / 'pkg/h264')
    stage_text = index_before.decode()
    anchor = '## Diagnostic only: initial motion candidates'
    if anchor in stage_text:
        stage_text = stage_text.replace(anchor, SECTION + anchor, 1)
    else:
        anchor = 'The [index audit]'
        need(anchor in stage_text, 'current round13 README has no stable insertion anchor')
        stage_text = stage_text.replace(anchor, SECTION + anchor, 1)
    STAGE.mkdir()
    shutil.copytree(SRC, STAGE / 'frozen-evidence')
    shutil.copy2(README, STAGE / 'source-review-README.txt')
    shutil.copy2(Path(__file__), STAGE / 'archive_bswrite_flush_precheck.py')
    (STAGE / 'root-source-records').mkdir()
    for _, stem, _, _ in VARIANTS:
        shutil.copy2(R / (stem + '-source.sha256'), STAGE / 'root-source-records')
    (STAGE / 'previous-round13-README.md').write_bytes(index_before)
    shutil.copy2(ROUND / 'archive-files.sha256', STAGE / 'previous-round13-archive-files.sha256')
    put_json('root-reported-commands.json', commands)
    put_json('decision.json', {
        'status': 'held_before_timing', 'candidate_merged': False,
        'compiler_inline_costs': [83, 84], 'compiler_inline_budget': 80,
        'correctness_tests_run': False, 'benchmarks_run': False,
        'native_or_full_frame_measurement_run': False,
        'unrun_drafts_are_not_validated': True,
        'reason': 'Both simple pointer-preserving slow-path extractions failed the inlineability gate; no third tuning iteration.'})
    (STAGE / 'README.md').write_text('''# Common bit-writer flush extraction: held before timing

This source experiment stopped at its compiler gate. Both straightforward forms preserve the original pointer-based word-store sequence but fail to make `BsWriteBits` inlineable. Main was not changed, and no speedup is claimed.

| Form | Reported inline cost | Budget | Result |
| --- | ---: | ---: | --- |
| Original fast branch plus void flush helper | 83 | 80 | Not inlineable |
| Inverse branch returning the flush helper's result | 84 | 80 | Not inlineable |

The two root-run `go build -gcflags=...=-m=2` commands exited successfully; their compiler diagnostics reject inlineability. These builds did not execute tests or benchmarks. Exact reported commands, explicit environment overrides, unrecorded inherited-environment scope, and source hashes are in [root command provenance](root-reported-commands.json). The archive helper does not rerun them.

[First source](frozen-evidence/first-pointer-void.go.txt), [second source](frozen-evidence/second-pointer-return.go.txt), and the [frozen round12 baseline](frozen-evidence/baseline.go.txt) are preserved verbatim. Both [first log](frozen-evidence/bswrite-flush-inline-precheck.txt) and [second log](frozen-evidence/bswrite-flush-inline-return-precheck.txt) retain all diagnostics; root's before/after source-hash records are copied separately. The [source manifest](frozen-evidence/manifest.json) and [original handoff](source-review-README.txt) identify the complete source scope.

The four reference/test/benchmark drafts under `frozen-evidence/unrun-drafts/` are unfinished and **unvalidated**. They were not compiled as tests or executed, and complete macroblock-syntax benchmark fixtures were not finished after the compiler gate failed. The common bit writer is distinct from the held CAVLC emitter; no CAVLC production source changed.

This is a pre-timing hold, separate from the earlier measured component experiments in [round13](../README.md), which remain held. No component timing, stream-parity extension, native comparison, frame timing, or live RDP acceleration follows from this check. [Decision](decision.json), [preservation audit](archive-audit.json), [file hashes](archive-files.sha256).
''')
    # Verify archived copies against frozen source evidence before publishing.
    expected_evidence = dict(manifest['archive_sha256'])
    expected_evidence['manifest.json'] = MANIFEST_SHA
    need(hashes(STAGE / 'frozen-evidence') == expected_evidence,
         'copied frozen evidence differs from its original manifest')
    need(sha(STAGE / 'source-review-README.txt') == README_SHA, 'copied source README changed')
    for command in commands:
        need(sha(STAGE / command['source_record']) == command['source_record_sha256'],
             f"copied root source-hash record changed: {command['name']}")
    # All old root files and protected state must still match immediately before publish.
    current = hashes(ROUND)
    current = {k: v for k, v in current.items() if not k.startswith(STAGE.name + '/')}
    need(current == old, 'round13 changed concurrently; leave staging for inspection')
    need(hashes(REPO / 'pkg/h264') == main_before, 'main H264 changed during archive preparation')
    need(all(sha(Path(path)) == value for path, value in protected.items()), 'parent/index changed')
    put_json('archive-audit.json', {
        'created_utc': datetime.now(timezone.utc).isoformat(), 'status': 'held_before_timing',
        'input_manifest_sha256': MANIFEST_SHA, 'source_readme_sha256': README_SHA,
        'prior_round13_files': old, 'allowed_existing_changes': ['README.md', 'archive-files.sha256'],
        'prior_round13_readme_sha256': old['README.md'],
        'new_round13_readme_sha256': hashlib.sha256(stage_text.encode()).hexdigest(),
        'main_h264_files_preserved': main_before, 'protected_parent_and_git_index_sha256': protected,
        'preservation_scope': 'All files present at runtime, including any earlier MVC archive; old nested archives are byte-preserved.',
        'helper_executes_no_compiler_test_or_benchmark': True})
    (STAGE / 'archive-files.sha256').write_text(inventory(STAGE))
    STAGE.rename(DEST)
    (ROUND / 'README.md').write_text(stage_text)
    (ROUND / 'archive-files.sha256').write_text(inventory(ROUND))
    for name, digest in old.items():
        if name not in {'README.md', 'archive-files.sha256'}:
            need(sha(ROUND / name) == digest, f'prior archive changed: {name}')
    need(hashes(REPO / 'pkg/h264') == main_before, 'main H264 changed')
    need(all(sha(Path(path)) == value for path, value in protected.items()), 'parent/index changed')
    need(parse_inventory(DEST / 'archive-files.sha256') == hashes(DEST, ('archive-files.sha256',)), 'component inventory mismatch')
    need(parse_inventory(ROUND / 'archive-files.sha256') == hashes(ROUND, ('archive-files.sha256',)), 'round13 inventory mismatch')
    print(json.dumps({'complete': True, 'archive': str(DEST), 'decision': 'held_before_timing',
                      'main_and_prior_archives_preserved': True, 'compiler_inline_costs': [83, 84]}, indent=2))


if __name__ == '__main__':
    main()
