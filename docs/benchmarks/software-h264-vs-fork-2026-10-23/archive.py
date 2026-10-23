#!/usr/bin/env python3
"""Archive text evidence, leaving binaries, raw video and encoded payloads in /tmp."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess


def sha(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--repo', type=Path, required=True)
    args = parser.parse_args()
    work, repo = args.work.resolve(), args.repo.resolve()
    destination = Path(__file__).resolve().parent
    assert (work / 'measure/complete.json').exists()
    source_hashes = json.loads((work / 'build/go-sources.json').read_text())
    mismatches = [name for name, expected in source_hashes.items() if sha(repo / name) != expected]
    if mismatches:
        raise RuntimeError(f'Go source changed during experiment: {mismatches}')
    native_status = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=no'],
                                           cwd=work / 'native-src', text=True)
    if native_status:
        raise RuntimeError(f'native tracked source changed: {native_status}')
    omitted = []
    for folder in ['build', 'preflight', 'measure']:
        target = destination / 'results' / folder
        target.mkdir(parents=True, exist_ok=True)
        for file in (work / folder).iterdir():
            if not file.is_file():
                continue
            if file.suffix in ['.json', '.txt', '.log', '.csv', '.framemd5', '.cpp']:
                shutil.copyfile(file, target / file.name)
            else:
                omitted.append(dict(path=str(file), bytes=file.stat().st_size, sha256=sha(file)))
    for name in ['go.test', 'native_bench', 'native-src/libopenh264.a']:
        file = work / name
        omitted.append(dict(path=str(file), bytes=file.stat().st_size, sha256=sha(file)))
    for name in ['cases.json', 'fixtures.json', 'summary.json', 'benchstat.txt', 'table.md']:
        shutil.copyfile(work / name, destination / name)
    (destination / 'omitted-payloads.json').write_text(json.dumps(omitted, indent=2) + '\n')
    (destination / 'source-verification.json').write_text(json.dumps(dict(
        go_files_verified=len(source_hashes), go_mismatches=mismatches,
        native_tracked_status=native_status), indent=2) + '\n')
    print(json.dumps(dict(archived_text_files=len(list((destination / 'results').rglob('*'))),
                         verified_go_files=len(source_hashes), omitted_payloads=len(omitted))))


if __name__ == '__main__':
    main()
