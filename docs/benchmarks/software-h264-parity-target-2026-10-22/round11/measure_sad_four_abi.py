from pathlib import Path
import os
import shutil
import subprocess

r = Path(__file__).resolve().parent
out = r / 'sad-four-abi-review'
out.mkdir(exist_ok=True)
candidate = r / 'sad-four-abi-next'
base = r / 'sad-four-abi-round10-base'
if not base.exists():
    shutil.copytree(r / 'round10', base)
for rel in ['pkg/h264/internal/common/sad_four_pointer_amd64_test.go',
            'pkg/h264/internal/encoder/diamond_sad_four_caller_bench_amd64_test.go',
            'pkg/h264/internal/encoder/diamond_sad_four_caller_amd64_test.go']:
    shutil.copy2(candidate / rel, base / rel)
env = os.environ | {'PATH': '/snap/go/current/bin:' + os.environ['PATH'],
                    'CGO_ENABLED': '0', 'GOMAXPROCS': '4'}
for kind, src in [('base', base), ('after', candidate)]:
    print('test/build ' + kind, flush=True)
    with (out / (kind + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', './pkg/h264/internal/common', './pkg/h264/internal/encoder', '-count=1'],
                       cwd=src, env=env, stdout=f, stderr=subprocess.STDOUT, check=True)
    for package in ['common', 'encoder']:
        subprocess.run(['go', 'test', '-c', '-o', str(out / (kind + '-' + package + '.test')),
                        './pkg/h264/internal/' + package], cwd=src, env=env, check=True)
    (out / (kind + '.txt')).write_text('')
with (out / 'after-race-tests.txt').open('w') as f:
    subprocess.run(['go', 'test', '-race', './pkg/h264/internal/common', './pkg/h264/internal/encoder',
                    '-run=TestSampleSadFour|TestDiamondSadFour', '-count=1'], cwd=candidate,
                   env=env | {'CGO_ENABLED': '1'}, stdout=f, stderr=subprocess.STDOUT, check=True)
for rep in range(6):
    for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
        for package, pattern in [('common', '^BenchmarkSampleSadFourRows$'),
                                 ('encoder', '^BenchmarkDiamondSadFourCaller$')]:
            result = subprocess.run(['taskset', '-c', '2', str(out / (kind + '-' + package + '.test')),
                                     '-test.run=^$', '-test.bench=' + pattern,
                                     '-test.benchtime=200ms', '-test.benchmem'],
                                    env=env, capture_output=True, text=True, check=True)
            with (out / (kind + '.txt')).open('a') as f:
                f.write(result.stdout)
    print('SAD-four checked/caller pair ' + str(rep + 1), flush=True)
with (out / 'benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat', str(out / 'base.txt'), str(out / 'after.txt')],
                   stdout=f, check=True)
