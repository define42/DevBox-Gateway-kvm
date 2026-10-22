from pathlib import Path
import os, shutil, subprocess

r = Path(__file__).resolve().parent
out = r / 'intra-dc-review'
out.mkdir(exist_ok=True)
candidate = r / 'intra-dc-simd-next'
base = r / 'intra-dc-round7-base'
if not base.exists():
    shutil.copytree(r / 'round7', base)
package = Path('pkg/h264/internal/encoder')
for name in ['intra_dc_bench_test.go', 'intra_dc_caller_bench_test.go']:
    shutil.copy2(candidate / package / name, base / package / name)
shutil.copy2(candidate / 'intra_dc_baseline_shim_test.go.txt',
             base / package / 'intra_dc_baseline_shim_test.go')
env = os.environ | {'PATH': '/snap/go/current/bin:' + os.environ['PATH'],
                    'CGO_ENABLED': '0', 'GOMAXPROCS': '4'}
for kind, src in [('base', base), ('after', candidate)]:
    print('test/build ' + kind, flush=True)
    with (out / (kind + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', './pkg/h264/internal/encoder', '-run=^TestIntraDC', '-count=1'],
                       cwd=src, env=env, stdout=f, stderr=subprocess.STDOUT, check=True)
    subprocess.run(['go', 'test', '-c', '-o', str(out / (kind + '.test')),
                    './pkg/h264/internal/encoder'], cwd=src, env=env, check=True)
    (out / (kind + '.txt')).write_text('')
for rep in range(6):
    for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
        result = subprocess.run(['taskset', '-c', '2', str(out / (kind + '.test')),
                                 '-test.run=^$', '-test.bench=^Benchmark(IntraDCTransforms|I16Reconstruction)$',
                                 '-test.benchtime=200ms', '-test.benchmem'],
                                env=env, capture_output=True, text=True, check=True)
        with (out / (kind + '.txt')).open('a') as f:
            f.write(result.stdout)
    print('intra DC caller/transform pair ' + str(rep + 1), flush=True)
with (out / 'benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat', str(out / 'base.txt'), str(out / 'after.txt')],
                   stdout=f, check=True)
