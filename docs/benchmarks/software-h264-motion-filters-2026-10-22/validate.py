#!/usr/bin/env python3
import json,os,subprocess
from pathlib import Path
r=Path(__file__).resolve().parent
repo=Path('/home/define42/git/DevBox-Gateway-kvm')
go='/snap/go/current/bin/go'
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH']}
packages=['./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc']
steps=[('tests',[go,'test','./pkg/h264/...','./internal/rdp/avc'],{}),('race',[go,'test','-race',*packages],{'CGO_ENABLED':'1'}),('fallback-avx2',[go,'test',*packages],{'GODEBUG':'cpu.avx2=off'}),('fallback-all',[go,'test',*packages],{'GODEBUG':'cpu.all=off'})]
for package in packages:
    name=package.rsplit('/',1)[-1]
    steps.append(('arm64-'+name,[go,'test','-c','-o',str(r/f'arm64-{name}.test'),package],{'GOARCH':'arm64','CGO_ENABLED':'0'}))
changed=sorted(str(p.relative_to(repo)) for p in (repo/'pkg/h264/internal/common').glob('mc_refine*.go'))
steps.append(('gopls',['/home/define42/go/bin/gopls','check','pkg/h264/internal/common/mc_avx2_amd64.go',*changed],{}))
records=[]
for name,cmd,extra in steps:
    print(name,flush=True)
    with (r/f'validation-{name}.txt').open('w') as f:
        f.write(' '.join(cmd)+'\n');f.flush()
        result=subprocess.run(cmd,cwd=repo,env=env|extra,stdout=f,stderr=subprocess.STDOUT)
    records.append(dict(name=name,command=cmd,environment=extra,exit_code=result.returncode))
    (r/'validation.json').write_text(json.dumps(records,indent=2)+'\n')
    if result.returncode:
        print((r/f'validation-{name}.txt').read_text());raise SystemExit(result.returncode)
print('All validation steps passed.',flush=True)
