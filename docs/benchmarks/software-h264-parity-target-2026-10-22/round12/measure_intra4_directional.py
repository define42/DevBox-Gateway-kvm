from pathlib import Path
import os
import shutil
import subprocess

r = Path(__file__).resolve().parent
out = r / 'intra4-directional-review'
out.mkdir(exist_ok=True)
candidate = r / 'intra4-directional-next'
base = r / 'intra4-directional-round11-base'
if not base.exists():
    shutil.copytree(r / 'round11', base)
for name in ['intra4_directional_bench_test.go', 'intra4_sad_bench_test.go']:
    rel = Path('pkg/h264/internal/encoder') / name
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
for name, options, extra in [('race', ['-race'], {'CGO_ENABLED': '1'}),
                              ('features-off', [], {'GODEBUG': 'cpu.all=off'})]:
    with (out / ('after-' + name + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', *options, './pkg/h264/internal/encoder',
                        '-run=^TestIntra4Directional', '-count=1'], cwd=candidate,
                       env=env | extra, stdout=f, stderr=subprocess.STDOUT, check=True)
for rep in range(6):
    for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
        result = subprocess.run(['taskset', '-c', '2', str(out / (kind + '.test')),
                                 '-test.run=^$', '-test.bench=^Benchmark(Intra4Directional|Intra4SadCaller)$',
                                 '-test.benchtime=200ms', '-test.benchmem'],
                                env=env, capture_output=True, text=True, check=True)
        with (out / (kind + '.txt')).open('a') as f:
            f.write(result.stdout)
    print('Directional I4 checked/caller pair ' + str(rep + 1), flush=True)
with (out / 'benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat', str(out / 'base.txt'), str(out / 'after.txt')],
                   stdout=f, check=True)
print('Directional I4 component comparison complete', flush=True)
