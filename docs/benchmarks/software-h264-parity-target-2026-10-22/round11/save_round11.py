from pathlib import Path
import json
import shutil
import subprocess

r = Path(__file__).resolve().parent
repo = Path('/home/define42/git/DevBox-Gateway-kvm')
out = repo / 'docs/benchmarks/software-h264-parity-target-2026-10-22/round11'
out.mkdir(parents=True, exist_ok=True)
files = ['codec-summary.json', 'codec-benchstat.txt', 'codec-native-benchstat.txt',
         'full-summary.json', 'full-benchstat.txt', 'round11-sources.sha256',
         'before-sources.sha256', 'validation.json', 'measure.py', 'measure_full.py',
         'measure_round11.py', 'summarize_codec.py', 'summarize_full.py', 'cases.json',
         'validate.py', 'before-overlay.json', 'round11-overlay.json',
         'measure_sad_four_abi.py', 'measure_update_mv.py',
         'sad-four-abi-next-README.txt', 'update-mv-simd-next-README.txt',
         'round11-integration.txt', 'round11-main-merged.txt',
         'profile_round.py', 'finish_round11_profiles.py',
         'save_round11.py', 'write_round11_report.py', 'round11-profile-scope.txt']
files += [p.name for p in r.glob('round11-*') if p.suffix in ('.txt', '.json', '.pprof')]
files += [p.name for p in r.glob('validation-*.txt')]
for name in sorted(set(files)):
    shutil.copy2(r / name, out / name)
for prefix, folder, kinds in [('codec', 'measure', ['before', 'after', 'native']),
                              ('full', 'full', ['before', 'after'])]:
    for name in kinds:
        shutil.copy2(r / folder / (name + '.txt'), out / (prefix + '-' + name + '.txt'))
shutil.copy2(r / 'parity/parity.txt', out / 'parity.txt')
for name in ['compare_test.go', 'fullscreen_test.go']:
    shutil.copy2(r / name, out / (name + '.txt'))
for folder in ['sad-four-abi-review', 'update-mv-review']:
    dest = out / folder
    dest.mkdir(exist_ok=True)
    for path in (r / folder).iterdir():
        if path.is_file() and path.suffix in ('.txt', '.log', '.json'):
            shutil.copy2(path, dest / path.name)
patch = subprocess.check_output(['git', 'diff', '641af9e6216055b6a9d918bdcba4ea7d40020750', '--', 'pkg/h264'], cwd=repo)
for name in subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', 'pkg/h264'],
                                    cwd=repo, text=True).splitlines():
    result = subprocess.run(['git', 'diff', '--no-index', '--', '/dev/null', name],
                            cwd=repo, stdout=subprocess.PIPE)
    assert result.returncode in (0, 1)
    patch += result.stdout
(out / 'encoder-change.patch').write_bytes(patch)
info = json.loads((out.parent / 'round10/build-info.json').read_text())
info.update(codec_before='frozen round10 (see ../round10/round10-sources.sha256)',
            retained_groups=33, component_benchtime='200ms; six alternating pairs')
(out / 'build-info.json').write_text(json.dumps(info, indent=2) + '\n')
print(out)
