from pathlib import Path
import os, shutil, subprocess

r = Path(__file__).resolve().parent
out = r / 'cavlc-trailing-review'
out.mkdir(exist_ok=True)
candidate = r / 'cavlc-trailing-next'
base = r / 'cavlc-trailing-round10-base'
if not base.exists():
    shutil.copytree(r / 'round10', base)
package = Path('pkg/h264/internal/encoder')
for name in ['cavlc_scratch_bench_test.go', 'cavlc_native_full_test.go', 'cavlc_native_full_bench_test.go', 'cavlc_trailing_test.go']:
    shutil.copy2(candidate / package / name, base / package / name)
env = os.environ | {'PATH': '/snap/go/current/bin:' + os.environ['PATH'],
                    'CGO_ENABLED': '0', 'GOMAXPROCS': '4',
                    'CAVLC_NATIVE_BINARY': str(r / 'cavlc-native-review/native_cavlc'),
                    'CAVLC_NATIVE_FIXTURES': str(r / 'cavlc-native-review/go/pkg/h264/internal/encoder/testdata/cavlc_native_fixtures.txt')}
for kind, src in [('base', base), ('after', candidate)]:
    print('test/build ' + kind, flush=True)
    with (out / (kind + '-tests.txt')).open('w') as f:
        subprocess.run(['go', 'test', './pkg/h264/internal/encoder', '-run=^TestCavlc', '-count=1'],
                       cwd=src, env=env | {'CAVLC_NATIVE_FIXTURES': str(r / 'cavlc-single-next/pkg/h264/internal/encoder/testdata/cavlc_native_single_fixtures.txt')}, stdout=f, stderr=subprocess.STDOUT, check=True)
    if kind == 'after':
        with (out / 'after-fallback-tests.txt').open('w') as f:
            subprocess.run(['go', 'test', './pkg/h264/internal/encoder', '-run=^TestCavlcTrailing', '-count=1'],
                           cwd=src, env=env | {'GODEBUG': 'cpu.all=off'}, stdout=f, stderr=subprocess.STDOUT, check=True)
    subprocess.run(['go', 'test', '-c', '-o', str(out / (kind + '.test')),
                    './pkg/h264/internal/encoder'], cwd=src, env=env, check=True)
    (out / (kind + '.txt')).write_text('')
for rep in range(6):
    for kind in (['base', 'after'] if rep % 2 == 0 else ['after', 'base']):
        for pattern in ['^Benchmark(CavlcMacroblock|CavlcLevelWriter)$',
                        '^BenchmarkCavlcNativeFull$/^end(3|14|15)$/^(zero|known-zero|sparse|dense|mixed)$/^prefix17$']:
            result = subprocess.run(['taskset', '-c', '2', str(out / (kind + '.test')),
                                     '-test.run=^$', '-test.bench=' + pattern,
                                     '-test.benchtime=200ms', '-test.benchmem'],
                                    env=env, capture_output=True, text=True, check=True)
            with (out / (kind + '.txt')).open('a') as f:
                f.write(result.stdout)
    print('CAVLC trailing caller/public pair ' + str(rep + 1), flush=True)
with (out / 'benchstat.txt').open('w') as f:
    subprocess.run(['/home/define42/go/bin/benchstat', str(out / 'base.txt'), str(out / 'after.txt')],
                   stdout=f, check=True)
