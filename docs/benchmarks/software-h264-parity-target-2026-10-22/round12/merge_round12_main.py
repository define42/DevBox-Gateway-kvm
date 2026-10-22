#!/usr/bin/env python3
"""Merge only the fully validated round12 H264 delta, preserving Git staging."""
from pathlib import Path
import hashlib,json,shutil,subprocess
r=Path(__file__).resolve().parent
repo=Path('/home/define42/git/DevBox-Gateway-kvm')
review=r/'round12-review';base=r/'round11';source=review/'source'
complete=json.loads((review/'complete.json').read_text())
assert complete['complete'] and complete['decoded_frames']==960 and complete['source_and_inputs_stable']
changes=json.loads((review/'changes-vs-round11.json').read_text())
assert changes and all(x['path'].startswith('pkg/h264/') and x['status'] in ('added','modified') for x in changes)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def tree(p):return {str(f.relative_to(p)):sha(f) for f in (p/'pkg/h264').rglob('*') if f.is_file()}
assert tree(repo)==tree(base),'Main H264 changed since round11; refusing blind merge'
for x in changes:
    rel=x['path'];target=repo/rel
    assert sha(source/rel)==x['after_sha256'],rel
    assert (sha(target) if target.exists() else None)==x['before_sha256'],rel
for x in changes:
    target=repo/x['path'];target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(source/x['path'],target)
assert tree(repo)==tree(source),'Main must exactly match validated H264 snapshot'
subprocess.run(['git','diff','--check','641af9e6216055b6a9d918bdcba4ea7d40020750','--','pkg/h264'],cwd=repo,check=True)
(r/'round12-main-merged.json').write_text(json.dumps({'files':changes,'h264_file_count':len(tree(repo)),'source':str(source),'validated_complete':complete,'git_index_untouched':True},indent=2)+'\n')
print('Merged',len(changes),'validated files; Git index untouched.')
