from pathlib import Path
import os
import shutil
import subprocess

r = Path(__file__).resolve().parent
out = r / 'update-mv-review'
out.mkdir(exist_ok=True)
candidate = r / 'update-mv-simd-next'
base = r / 'update-mv-round10-base'
if not base.exists():
    shutil.copytree(r / 'round10', base)
rel = Path('pkg/h264/internal/encoder/update_mv_bench_test.go')
shutil.copy2(candidate / rel, base / rel)
env = os.environ | {'PATH': '/snap/go/current/bin:' + os.environ['PATH'],
                    'CGO_ENABLED': '0', 'GOMAXPROCS': '4'}
for kind, src in [('base', base), ('after', candidate)]:
    print('test/build ' + kind, flush=True)
    with (out / (kind + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', './pkg/h264/internal/encoder', '-count=1'],
                       cwd=src, env=env, stdout=f, stderr=subprocess.STDOUT, check=True)
    subprocess.run(['go', 'test', '-c', '-o', str(out / (kind + '.test')),
                    './pkg/h264/internal/encoder'], cwd=src, env=env, check=True)
    (out / (kind + '.txt')).write_text('')
for name, args, extra in [('race', ['-race'], {'CGO_ENABLED': '1'}),
                          ('features-off', [], {'GODEBUG': 'cpu.all=off'})]:
    with (out / ('after-' + name + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', *args, './pkg/h264/internal/encoder',
                        '-run=^Test(UpdateMbMv|IntraVaaSIMDDispatch)', '-count=1'], cwd=candidate,
                       env=env | extra, stdout=f, stderr=subprocess.STDOUT, check=True)
for rep in range(6):
    for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
        result = subprocess.run(['taskset', '-c', '2', str(out / (kind + '.test')),
                                 '-test.run=^$', '-test.bench=^BenchmarkUpdateMbMv(SkipCaller)?$',
                                 '-test.benchtime=200ms', '-test.benchmem'],
                                env=env, capture_output=True, text=True, check=True)
        with (out / (kind + '.txt')).open('a') as f:
            f.write(result.stdout)
    print('Motion-vector fill/caller pair ' + str(rep + 1), flush=True)
with (out / 'benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat', str(out / 'base.txt'), str(out / 'after.txt')],
                   stdout=f, check=True)
