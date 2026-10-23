#!/usr/bin/env python3
"""Build benchmark binaries from the current Go tree and a prepared native archive."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--repo', type=Path, required=True)
    parser.add_argument('--work', type=Path, required=True)
    args = parser.parse_args()
    repo, work = args.repo.resolve(), args.work.resolve()
    source = Path(__file__).resolve().parent
    native = work / 'native-src'
    build = work / 'build'
    build.mkdir(exist_ok=True)
    metadata = dict(go_revision=subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip(),
                    native_revision=subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=native, text=True).strip(),
                    initial_worktree_status=subprocess.check_output(['git', 'status', '--short'], cwd=repo, text=True),
                    commands=[])
    go_src = work / 'compare_test.go'
    shutil.copyfile(source / 'compare_test.go.txt', go_src)
    overlay = work / 'overlay.json'
    virtual = repo / 'internal/rdp/avc/z_fork_comparison_test.go'
    assert not virtual.exists()
    overlay.write_text(json.dumps({'Replace': {str(virtual): str(go_src)}}, indent=2) + '\n')
    env = dict(os.environ, CGO_ENABLED='0', GOTOOLCHAIN='local', GOMAXPROCS='4', GOAMD64='v1')
    commands = [
        ('go-build', ['go', 'test', '-p=2', f'-overlay={overlay}', '-c', '-o', str(work / 'go.test'), './internal/rdp/avc']),
        ('native-harness-build', ['g++', '-O3', '-std=c++17', '-I', str(native / 'codec/api/wels'),
                                 str(source / 'native_bench.cpp'), str(native / 'libopenh264.a'),
                                 '-pthread', '-o', str(work / 'native_bench')]),
    ]
    for name, command in commands:
        result = subprocess.run(command, cwd=repo, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        (build / f'{name}.log').write_text(result.stdout)
        metadata['commands'].append(dict(command=command, cwd=str(repo), returncode=result.returncode))
        if result.returncode:
            raise RuntimeError(f'{name} failed: {result.stdout}')
    metadata['go_build_environment'] = {key: env[key] for key in ['CGO_ENABLED', 'GOTOOLCHAIN', 'GOMAXPROCS', 'GOAMD64']}
    metadata['go_binary_info'] = subprocess.check_output(['go', 'version', '-m', str(work / 'go.test')], text=True)
    metadata['native_dynamic_libraries'] = subprocess.check_output(['ldd', str(work / 'native_bench')], text=True)
    metadata['host'] = subprocess.check_output(['lscpu'], text=True)
    metadata['cpu_governors'] = {str(p): p.read_text().strip() for p in Path('/sys/devices/system/cpu').glob('cpu*/cpufreq/scaling_governor')}
    metadata['binary_sha256'] = {name: hashlib.sha256((work / name).read_bytes()).hexdigest() for name in ['go.test', 'native_bench']}
    files = sorted([repo / 'go.mod', repo / 'go.sum'] + list((repo / 'pkg/h264').rglob('*')) + list((repo / 'internal/rdp/avc').rglob('*')))
    hashes = {str(p.relative_to(repo)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files if p.is_file()}
    (build / 'go-sources.json').write_text(json.dumps(hashes, indent=2) + '\n')
    (build / 'harness-build.json').write_text(json.dumps(metadata, indent=2) + '\n')
    print(json.dumps(metadata['binary_sha256'], indent=2))


if __name__ == '__main__':
    main()
