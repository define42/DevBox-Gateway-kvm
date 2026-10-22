#!/usr/bin/env python3
"""Archive completed round12 evidence after merge; never execute tests or builds."""
from pathlib import Path
import gzip
import hashlib
import io
import json
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
DEST = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round12'
RUN = ROOT / 'round12-review'
ORIGINAL = '641af9e6216055b6a9d918bdcba4ea7d40020750'
COMPRESS = {'base-full-encoder-tests.txt', 'after-corrected-full-encoder-tests.txt'}


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def tree(path, prefix=None):
    base = path / prefix if prefix else path
    return {str(p.relative_to(path)): sha(p) for p in sorted(base.rglob('*')) if p.is_file()}


def read_json(path):
    return json.loads(path.read_text())


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def source_manifest(path):
    return dict((line.split(None, 1)[1].strip(), line.split(None, 1)[0])
                for line in path.read_text().splitlines() if line.strip())


def main():
    complete = read_json(RUN / 'complete.json')
    assert complete['complete'] and complete['source_and_inputs_stable']
    assert complete['decoded_frames'] == 960 and complete['codec_cases'] == 10 and complete['full_cases'] == 6
    assert complete['codec_samples_per_kind'] == 60 and complete['full_samples_per_kind'] == 36
    merged = read_json(ROOT / 'round12-main-merged.json')
    assert merged['h264_file_count'] == 573 and merged['git_index_untouched']
    expected = {p: h for p, h in source_manifest(RUN / 'current-sources.sha256').items() if p.startswith('pkg/h264/')}
    assert len(expected) == 573
    assert tree(REPO, 'pkg/h264') == tree(RUN / 'source', 'pkg/h264') == expected, 'Main differs from the measured source'
    changes = read_json(RUN / 'changes-vs-round11.json')
    assert len(changes) == 8 and all(x['path'].startswith('pkg/h264/internal/encoder/') for x in changes)
    assert any(x['path'].endswith('intra4_directional_amd64.s') for x in changes)
    assert all('cavlc_emit' not in x['path'] for x in changes), 'Held emitter must not enter selected source'
    scratch = DEST / 'scratch-isolation-review'
    assert scratch.is_dir(), 'Existing held scratch archive is required'
    scratch_before = tree(scratch)
    index_path = REPO / '.git/index'
    index_before = sha(index_path)
    stage = Path(tempfile.mkdtemp(prefix='round12-archive-stage-', dir=ROOT))
    records, omitted, compressed = [], [], []

    def copy(source, relative, compress=False):
        target = stage / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        digest = sha(source)
        if compress:
            assert source.name in COMPRESS and source.stat().st_size > 10_000_000
            target = target.with_name(target.name + '.gz')
            with source.open('rb') as src, target.open('wb') as raw:
                with gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as dst:
                    shutil.copyfileobj(src, dst)
            decoded = hashlib.sha256()
            size = 0
            with gzip.open(target, 'rb') as stream:
                for block in iter(lambda: stream.read(1024 * 1024), b''):
                    decoded.update(block)
                    size += len(block)
            assert decoded.hexdigest() == digest and size == source.stat().st_size
            compressed.append({'source': str(source), 'archive': str(target.relative_to(stage)),
                               'source_sha256': digest, 'source_bytes': size,
                               'gzip_sha256': sha(target), 'gzip_bytes': target.stat().st_size,
                               'decompression_verified': True, 'gzip_mtime': 0})
        else:
            shutil.copy2(source, target)
            assert sha(target) == digest
        records.append({'source': str(source), 'archive': str(target.relative_to(stage)),
                        'source_sha256': digest, 'source_bytes': source.stat().st_size,
                        'archive_sha256': sha(target), 'archive_bytes': target.stat().st_size})

    def omit(source, reason):
        omitted.append({'source': str(source), 'bytes': source.stat().st_size,
                        'sha256': sha(source), 'reason': reason})

    # Full snapshot is represented by exact fingerprints and a replay-checked patch.
    # Binaries/streams stay local; retain their hashes, sizes and omission reasons.
    for source in sorted(RUN.rglob('*')):
        if not source.is_file():
            continue
        rel = source.relative_to(RUN)
        if rel.parts[0] == 'source':
            continue
        if rel.parts[0] == 'bin' or source.suffix == '.264':
            omit(source, 'compiled binary or encoded stream; fingerprints and parity results archived')
            continue
        if source.read_bytes()[:4] == b'\x7fELF':
            raise AssertionError(f'Unexpected executable outside bin: {source}')
        archived = Path('run') / rel
        if source.suffix in {'.go', '.cpp', '.s', '.S'}:
            archived = archived.with_name(archived.name + '.txt')
        copy(source, archived)

    for folder in ['intra4-directional-review', 'cavlc-emitter-review']:
        for source in sorted((ROOT / folder).rglob('*')):
            if not source.is_file():
                continue
            rel = source.relative_to(ROOT / folder)
            with source.open('rb') as stream:
                magic = stream.read(4)
            if magic == b'\x7fELF' or source.suffix in {'.test', '.a', '.so', '.264', '.i420', '.rgba'}:
                omit(source, 'component executable or large payload; source/binary fingerprints retained')
                continue
            assert source.suffix in {'.txt', '.log', '.json', '.jsonl', '.md', '.py', '.sha256', '.patch'}, source
            copy(source, Path(folder) / rel, compress=(folder == 'cavlc-emitter-review' and source.name in COMPRESS))
    assert len(compressed) == 2

    for name in ['intra4-directional-next-README.txt', 'measure_intra4_directional.py',
                 'measure_round12-README.md', 'measure_round12.py', 'merge_round12_main.py',
                 'round12-main-merged.json', 'save_round12.py', 'write_round12_report.py']:
        copy(ROOT / name, Path(name))

    # Preserve both frozen and actually measured held source deltas. This captures
    # the unused-import repair and formatting without silently relabeling either.
    emitter = ROOT / 'cavlc-emitter-review'
    provenance = read_json(emitter / 'provenance.json')
    for label, base_key, candidate_key, base_hash, candidate_hash in [
        ('frozen', 'frozen_base', 'frozen_candidate', 'frozen-base-source-sha256.json', 'frozen-candidate-source-sha256.json'),
        ('measured', 'working_base', 'working_candidate', 'base-postformat-source-sha256.json', 'after-postformat-source-sha256.json'),
    ]:
        before, after = read_json(emitter / base_hash), read_json(emitter / candidate_hash)
        delta = []
        for rel in sorted(set(before) | set(after)):
            if before.get(rel) == after.get(rel):
                continue
            assert rel.startswith('pkg/h264/'), rel
            delta.append({'path': rel, 'before_sha256': before.get(rel), 'after_sha256': after.get(rel)})
            for kind, key, hashes in [('base', base_key, before), ('candidate', candidate_key, after)]:
                if rel in hashes:
                    source = Path(provenance[key]) / rel
                    assert sha(source) == hashes[rel], f'Held {label} source changed: {source}'
                    copy(source, Path('cavlc-emitter-review/source-delta') / label / kind / (rel + '.txt'))
        write_json(stage / f'cavlc-emitter-review/source-delta/{label}/changes.json', delta)

    # Explicit original baseline includes staged and unstaged tracked changes.
    # Add untracked files separately, then replay the result against pristine Git.
    patch = subprocess.check_output(['git', 'diff', '--binary', ORIGINAL, '--', 'pkg/h264'], cwd=REPO)
    untracked = subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', 'pkg/h264'], cwd=REPO, text=True).splitlines()
    for name in untracked:
        result = subprocess.run(['git', 'diff', '--binary', '--no-index', '--', '/dev/null', name], cwd=REPO, stdout=subprocess.PIPE, check=False)
        assert result.returncode in (0, 1)
        patch += result.stdout
    (stage / 'encoder-change.patch').write_bytes(patch)
    with tempfile.TemporaryDirectory(prefix='round12-patch-replay-', dir=ROOT) as temporary:
        replay = Path(temporary)
        original = subprocess.check_output(['git', 'archive', ORIGINAL, 'pkg/h264'], cwd=REPO)
        with tarfile.open(fileobj=io.BytesIO(original)) as archive:
            archive.extractall(replay, filter='data')
        subprocess.run(['git', 'apply', '--check', '--whitespace=error-all', str(stage / 'encoder-change.patch')], cwd=replay, check=True)
        subprocess.run(['git', 'apply', str(stage / 'encoder-change.patch')], cwd=replay, check=True)
        assert tree(replay, 'pkg/h264') == expected, 'Cumulative patch does not reproduce measured H264 source'
    assert tree(REPO, 'pkg/h264') == expected and sha(index_path) == index_before
    assert tree(scratch) == scratch_before
    write_json(stage / 'compression.json', compressed)
    write_json(stage / 'omitted-payloads.json', omitted)
    write_json(stage / 'archive-manifest.json', {'retained_groups': 34, 'h264_files': len(expected),
               'original_commit': ORIGINAL, 'complete_run': complete, 'copied_artifacts': records,
               'cumulative_patch_replayed': True, 'main_equals_measured_snapshot': True,
               'git_index_untouched': True, 'existing_scratch_archive': scratch_before,
               'source_snapshot_omitted': 'Exact source fingerprints and replay-checked cumulative patch retained',
               'scope': 'Directional predictors selected; initial CAVLC emitter and scratch isolation held'})
    # Publish only new top-level entries. Never modify the prior scratch archive.
    assert all(not (DEST / child.name).exists() for child in stage.iterdir()), 'Destination artifact already exists; refusing overwrite'
    DEST.mkdir(parents=True, exist_ok=True)
    for child in sorted(stage.iterdir()):
        shutil.move(str(child), DEST / child.name)
    stage.rmdir()
    assert tree(scratch) == scratch_before and sha(index_path) == index_before
    inventory = tree(DEST)
    inventory.pop('archive-files.sha256', None)
    (DEST / 'archive-files.sha256').write_text(''.join(f'{digest}  {name}\n' for name, digest in sorted(inventory.items())))
    print(DEST)


if __name__ == '__main__':
    main()
