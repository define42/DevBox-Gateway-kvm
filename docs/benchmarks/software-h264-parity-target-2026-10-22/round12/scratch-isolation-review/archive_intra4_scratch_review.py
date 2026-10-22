#!/usr/bin/env python3
"""Archive the isolated scratch experiment after both frame gates finish.

Source-only preparation: root decides when to execute. This helper does not build,
benchmark, merge code, alter old evidence, or decide retention. It creates one new
prospective round12/scratch-isolation-review directory and refuses overwrites.
"""
import difflib
import hashlib
import json
from pathlib import Path
import shutil

ROOT = Path(__file__).resolve().parent
REPO = Path('/home/define42/git/DevBox-Gateway-kvm')
OUT = REPO / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round12/scratch-isolation-review'
BASE = ROOT / 'round11'
CANDIDATE = ROOT / 'intra4-scratch-isolation-next'
REVIEWS = {
    'component': ROOT / 'intra4-scratch-review',
    'full-path': ROOT / 'intra4-scratch-frame-review',
    'codec': ROOT / 'intra4-scratch-codec-review',
}
CHANGED = [
    'pkg/h264/internal/encoder/svc_encode_slice.go',
    'pkg/h264/internal/encoder/intra4_scratch.go',
    'pkg/h264/internal/encoder/intra4_scratch_bench_test.go',
    'pkg/h264/internal/encoder/intra4_scratch_diagnostic_test.go',
    'pkg/h264/internal/encoder/intra4_scratch_test.go',
]
TEXT_SUFFIXES = {'.txt', '.log', '.json', '.sha256', '.py', '.md', '.go'}
PAYLOAD_SUFFIXES = {'.test', '.264', '.h264', '.i420', '.rgba', '.pprof'}
SCRIPTS = ['measure_intra4_scratch.py', 'measure_intra4_scratch_frames.py',
           'measure_intra4_scratch_codec.py', 'archive_intra4_scratch_review.py']


def digest(path):
    with path.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def load_json(path):
    return json.loads(path.read_text())


def verify_fingerprint(root, manifest):
    count = 0
    for line in manifest.read_text().splitlines():
        expected, name = line.split('  ', 1)
        assert digest(root / name) == expected, (root, name)
        count += 1
    return count


def make_patch():
    baseline_files = {str(f.relative_to(BASE)): f for f in (BASE / 'pkg/h264').rglob('*') if f.is_file()}
    candidate_files = {str(f.relative_to(CANDIDATE)): f for f in (CANDIDATE / 'pkg/h264').rglob('*') if f.is_file()}
    actual = {name for name in baseline_files.keys() | candidate_files.keys()
              if name not in baseline_files or name not in candidate_files
              or baseline_files[name].read_bytes() != candidate_files[name].read_bytes()}
    assert actual == set(CHANGED), ('Unexpected candidate source delta', sorted(actual))
    patch = []
    manifest = []
    for name in CHANGED:
        before, after = BASE / name, CANDIDATE / name
        before_text = before.read_text() if before.exists() else ''
        after_text = after.read_text()
        assert after_text.endswith('\n') and (not before_text or before_text.endswith('\n'))
        patch.append(f'diff --git a/{name} b/{name}\n')
        if not before.exists():
            patch.append('new file mode 100644\n')
        patch.extend(difflib.unified_diff(before_text.splitlines(keepends=True),
                                          after_text.splitlines(keepends=True),
                                          fromfile='a/' + name if before.exists() else '/dev/null',
                                          tofile='b/' + name, n=3))
        manifest.append({'path': name, 'baseline_sha256': digest(before) if before.exists() else None,
                         'candidate_sha256': digest(after), 'new_file': not before.exists()})
    return ''.join(patch), manifest


def copy_text(source, destination, copied):
    # Keep archived overlay sources from becoming packages under go test ./....
    if destination.suffix == '.go':
        destination = destination.with_name(destination.name + '.txt')
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)
    copied.append({'archive': str(destination.relative_to(OUT)), 'source': str(source),
                   'sha256': digest(destination), 'bytes': destination.stat().st_size})


def table(rows):
    lines = ['| Case | Round11 ms | Scratch ms | Change | p-value |',
             '| --- | ---: | ---: | ---: | ---: |']
    for row in rows:
        lines.append(f"| {row['case']} | {row['before']['ns/op']/1e6:.3f} | "
                     f"{row['after']['ns/op']/1e6:.3f} | {row['wall_change_percent']:+.2f}% | {row['p']:.3f} |")
    return '\n'.join(lines)


def classifications(rows):
    wins = sum(row['p'] < .05 and row['wall_change_percent'] < 0 for row in rows)
    regressions = sum(row['p'] < .05 and row['wall_change_percent'] > 0 for row in rows)
    return wins, regressions, len(rows) - wins - regressions


def main():
    assert not OUT.exists(), f'Refusing to overwrite existing archive: {OUT}'
    # Reject unfinished evidence. In particular, codec results must exist before archival.
    full_complete = load_json(REVIEWS['full-path'] / 'complete.json')
    codec_complete = load_json(REVIEWS['codec'] / 'complete.json')
    assert full_complete['parity_cases'] == 10 and full_complete['decoded_frames'] == 960
    assert full_complete['full_cases'] == 6 and full_complete['pairs_per_case'] == 6
    assert codec_complete['codec_cases'] == 10 and codec_complete['pairs_per_case'] == 6
    assert codec_complete['native_reruns'] == 0 and codec_complete['prior_full_results_preserved']
    assert (REVIEWS['component'] / 'benchstat.txt').is_file()
    full_rows = load_json(REVIEWS['full-path'] / 'full-summary.json')
    codec_rows = load_json(REVIEWS['codec'] / 'summary.json')
    assert len(full_rows) == 6 and len(codec_rows) == 10
    assert classifications(full_rows) == (0, 0, 6), 'Preserve and verify all six neutral full-path results'
    base_count = verify_fingerprint(BASE, ROOT / 'round11-sources.sha256')
    candidate_count = verify_fingerprint(CANDIDATE, REVIEWS['full-path'] / 'candidate-sources.sha256')
    patch, change_manifest = make_patch()
    for directory in REVIEWS.values():
        for source in directory.rglob('*'):
            if source.is_file():
                assert source.suffix in TEXT_SUFFIXES | PAYLOAD_SUFFIXES, ('Unclassified artifact', source)
                assert not source.is_symlink(), ('Unexpected symlink', source)
    OUT.mkdir(parents=True)
    copied, omitted = [], []
    for label, directory in REVIEWS.items():
        for source in sorted(directory.rglob('*')):
            if not source.is_file():
                continue
            relative = source.relative_to(directory)
            if source.suffix in PAYLOAD_SUFFIXES:
                omitted.append({'source': str(source), 'review': label, 'relative_path': str(relative),
                                'bytes': source.stat().st_size, 'sha256': digest(source),
                                'reason': 'Binary or stream/profile payload excluded; hash retained'})
            else:
                copy_text(source, OUT / label / relative, copied)
    for name in SCRIPTS:
        copy_text(ROOT / name, OUT / name, copied)
    for name in ['intra4-scratch-isolation-next-README.txt', 'cases.json', 'round11-sources.sha256']:
        copy_text(ROOT / name, OUT / name, copied)
    (OUT / 'scratch-isolation.patch').write_text(patch)
    (OUT / 'changed-files.json').write_text(json.dumps(change_manifest, indent=2) + '\n')
    (OUT / 'omitted-payloads.json').write_text(json.dumps(omitted, indent=2) + '\n')
    wins, regressions, neutral = classifications(codec_rows)
    readme = f'''# Intra4 scratch isolation experiment

This is an isolated experiment against [frozen round11](../../round11/README.md).
Archival does not select or merge the candidate. This helper makes no retention
decision. Component measurements do not establish a full-frame speedup or native
parity, and none of these harnesses measures the live RDP Encoding average.

The candidate places the 32-byte prediction buffer and two 16-byte mode buffers
inside one allocation with leading and trailing `cpu.CacheLinePad` guards. The
five-file [patch](scratch-isolation.patch) changes only allocation ownership and
adds tests, a complete I4 caller benchmark and real InitializeExt address diagnostics.
Exact slice lengths/capacities remain unchanged. The geometry assumption is the
architecture's x/sys padding size; this is not a universal hardware guarantee.
[Scope, source evidence and contracts](intra4-scratch-isolation-next-README.txt).

## Component and allocation evidence

[Component statistics](component/benchstat.txt), [baseline addresses](component/base-addresses.txt),
[candidate addresses](component/after-addresses.txt) and all raw samples are retained.
The diagnostic uses real one/four-slice InitializeExt allocations, not artificially
packed workers. Address sharing identifies placement; it alone does not measure
contention. One/four-worker complete I4 benchmarks have six alternating pairs,
200 ms per case, CPUs 2/0-3 and GOMAXPROCS=4. They measure caller throughput.

## Full EncodeDamage gate

All six results remain inconclusive. These data are retained in full and are not
replaced by the separate codec scope below. Before is frozen round11, not the
original project baseline. Six alternating pairs use 60 measured + 16 warmup frames,
30 FPS, CPUs 2/0-3, including conversion, damage preparation, encoding and owning
payload construction. Capture, socket/ACK waits and presentation are excluded.

{table(full_rows)}

[Raw/statistical results](full-path/full-benchstat.txt),
[exact summary](full-path/full-summary.json), [provenance](full-path/provenance.json).

## Distinct codec-only gate

The codec scope adds PDF, 720p and QP32 absent from the full-path case set. It uses
six adjacent alternating pairs for all ten standard cases, 80 measured + 16 warmup
frames, 60 FPS and the same affinity. It times encoding and owning payload only.
Native is not rerun. Classifications at p<0.05: {wins} reductions, {regressions}
regressions and {neutral} inconclusive cases. These classifications do not select
the experiment or supersede the six neutral full-path results.

{table(codec_rows)}

[Raw/statistical results](codec/benchstat.txt), [exact summary](codec/summary.json),
[scope and binary provenance](codec/provenance.json).

## Correctness and reproduction

The preflight checks ten candidate streams against saved original/native hashes.
FFmpeg decoded 960 frames; ffprobe independently counted 96 frames in each case.
[Parity/hash summary](full-path/parity/parity.json), [decoder records](full-path/parity/parity.txt).
Component package/race results are preserved with the component artifacts.
Binaries and stream payloads are excluded; [omitted-payloads.json](omitted-payloads.json)
retains their paths, sizes and hashes. Overlay files retain exact content with
`.go.txt` names; restore `.go` before adapting the measurement scripts' local paths.

The patch is against the complete [round11 source fingerprint](round11-sources.sha256),
not the original commit. [Changed-file hashes](changed-files.json) and the archived
[candidate fingerprint](full-path/candidate-sources.sha256) identify all source.
The three measurement scripts and this archival helper are saved here. Their
absolute workspace/fixture paths require adaptation elsewhere. The helper refuses
to overwrite its target, requires both gates to finish, and verifies the five-file
delta and fingerprints before copying evidence. [Archive manifest](archive-manifest.json).
'''
    (OUT / 'README.md').write_text(readme)
    archive_record = {
        'experiment': 'intra4 scratch cache-line isolation',
        'retention_decision': 'Not made by this archival helper',
        'baseline_source_files_verified': base_count,
        'candidate_source_files_verified': candidate_count,
        'changed_files': len(change_manifest), 'full_path_cases_retained': len(full_rows),
        'codec_cases_retained': len(codec_rows), 'source_mutations': False,
        'copied_artifacts': copied, 'omitted_payload_count': len(omitted),
    }
    (OUT / 'archive-manifest.json').write_text(json.dumps(archive_record, indent=2) + '\n')
    archive_hashes = [digest(f) + '  ' + str(f.relative_to(OUT))
                      for f in sorted(OUT.rglob('*')) if f.is_file()]
    (OUT / 'archive-files.sha256').write_text('\n'.join(archive_hashes) + '\n')
    print(f'Archived isolated evidence to {OUT}; no retention decision made')


if __name__ == '__main__':
    main()
