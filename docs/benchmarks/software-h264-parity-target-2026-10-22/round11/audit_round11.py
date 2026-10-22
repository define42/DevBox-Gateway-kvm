from pathlib import Path
import hashlib
import io
import json
import re
import shutil
import subprocess
import tarfile
import tempfile

r = Path(__file__).resolve().parent
repo = Path('/home/define42/git/DevBox-Gateway-kvm')
p = repo / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round11'
expected = {}
for line in (p / 'round11-sources.sha256').read_text().splitlines():
    digest, rel = line.split('  ', 1)
    expected[rel] = digest
    for root in [repo, r / 'round11']:
        assert hashlib.sha256((root / rel).read_bytes()).hexdigest() == digest, str(root / rel)
assert len(expected) == 566, len(expected)
assert all(x['exit_code'] == 0 for x in json.loads((p / 'validation.json').read_text()))
assert len(json.loads((p / 'codec-summary.json').read_text())) == 10
assert len(json.loads((p / 'full-summary.json').read_text())) == 6
assert len((p / 'parity.txt').read_text().splitlines()) == 10
for report in [p / 'README.md', p.parent / 'README.md']:
    for link in re.findall(r'\]\(([^)]+)\)', report.read_text()):
        if '://' not in link and not link.startswith('#') and link != 'artifact-audit.json':
            assert (report.parent / link.split('#')[0]).exists(), (report, link)
replay = Path(tempfile.mkdtemp(prefix='round11-patch-replay-', dir=r))
# The frozen measurement baseline adds five benchmark files; replay on the
# actual original Git tree so new-file patches are not mistaken for reversals.
archive = subprocess.check_output(['git', 'archive', '641af9e6216055b6a9d918bdcba4ea7d40020750', 'pkg/h264'], cwd=repo)
with tarfile.open(fileobj=io.BytesIO(archive)) as tree:
    tree.extractall(replay, filter='data')
result = subprocess.run(['patch', '--batch', '--fuzz=0', '-p1', '-i', str(p / 'encoder-change.patch')],
                        cwd=replay, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
(r / 'round11-patch-replay.txt').write_text(result.stdout)
assert result.returncode == 0, result.stdout[-3000:]
actual = {str(f.relative_to(replay)): hashlib.sha256(f.read_bytes()).hexdigest()
          for f in (replay / 'pkg/h264').rglob('*') if f.is_file()}
assert actual == expected, (len(actual), len(expected), set(actual) ^ set(expected))
result = subprocess.run(['git', 'diff', '--check', '641af9e6216055b6a9d918bdcba4ea7d40020750', '--', 'pkg/h264'],
                        cwd=repo, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
assert result.returncode == 0, result.stdout
record = {'source_files_verified': len(expected), 'main_equals_frozen_snapshot': True,
          'cumulative_patch_replays_on_original': True, 'report_links_exist': True,
          'validation_exit_codes_zero': True,
          'cumulative_whitespace_command': 'git diff --check 641af9e6216055b6a9d918bdcba4ea7d40020750 -- pkg/h264',
          'cumulative_whitespace_exit_code': result.returncode}
(p / 'artifact-audit.json').write_text(json.dumps(record, indent=2) + '\n')
shutil.copy2(__file__, p / 'audit_round11.py')
print(json.dumps(record, indent=2))
