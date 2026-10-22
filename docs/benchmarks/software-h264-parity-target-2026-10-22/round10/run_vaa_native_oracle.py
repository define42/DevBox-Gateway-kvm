from pathlib import Path
import os
import subprocess

r = Path(__file__).resolve().parent
out = r / 'vaa-native-oracle'
env = os.environ | {'CGO_ENABLED': '0', 'GOMAXPROCS': '4',
                    'VAA_NATIVE_ORACLE': str(out / 'native.txt')}

def run(name, command, run_env=env):
    print(name, flush=True)
    with (out / (name + '.log')).open('w') as f:
        subprocess.run(command, env=run_env, stdout=f, stderr=subprocess.STDOUT, check=True)

run('fixtures', ['python3', str(out / 'generate_fixtures.py')])
run('native-build', ['c++', '-O2', '-std=c++17', '-Wall', '-Wextra', '-Werror',
                     str(out / 'native_vaa.cpp'),
                     '/tmp/openh264-comparison-20261021/native/libopenh264.a',
                     '-lpthread', '-lm', '-o', str(out / 'native-vaa')])
run('native-parity', [str(out / 'native-vaa'), str(out / 'fixtures.txt'), str(out / 'native.txt')])
for name, tree, candidate in [('base', 'round9', False), ('candidate', 'vaa-avx2-next', True)]:
    overlay = out / (name + '-overlay.json')
    run(name + '-overlay', ['python3', str(out / 'make_overlay.py'), str(r / tree),
                            str(overlay)] + (['--avx2'] if candidate else []))
    command = ['/snap/go/current/bin/go', '-C', str(r / tree), 'test', '-overlay',
               str(overlay), '-tags', 'integration', './pkg/h264/internal/processing',
               '-run', '^TestVAANativeOutputOracle$', '-count=1', '-v']
    run(name + '-parity', command)
    if candidate:
        run(name + '-avx2-off-parity', command, env | {'GODEBUG': 'cpu.avx2=off'})
print('Native VAA oracle validation complete', flush=True)
