#!/usr/bin/env python3
"""Run final validation outside benchmark timing, preserving every command and log."""
from pathlib import Path
import argparse,json,os,subprocess,time
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--root',type=Path,required=True)
args=parser.parse_args()
root=args.root.resolve()
source=root/'after'
out=root/'results/validation';out.mkdir(exist_ok=True)
env=os.environ|{'GOCACHE':str(root/'gocache'),'CGO_ENABLED':'0','GOWORK':'off'}
checks=[
 ('packages',['go','test','-count=1','./pkg/h264/...','./internal/rdp/...'],{}),
 ('conformance',['go','test','-count=1','-v','./pkg/h264/internal/encoder','-run','^TestEncoderOutputCompareOutput$'],{}),
 ('avx2-off',['go','test','-count=1','./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc'],{'GODEBUG':'cpu.avx2=off'}),
 ('cpu-off',['go','test','-count=1','./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc'],{'GODEBUG':'cpu.all=off'}),
 ('race',['go','test','-race','-count=1','./pkg/h264/internal/common','./pkg/h264/internal/encoder','./internal/rdp/avc'],{'CGO_ENABLED':'1'}),
 ('checkptr',['go','test','-gcflags=all=-d=checkptr=2','-count=1','./pkg/h264/internal/encoder','-run','TestWelsDctMb|TestDctLumaMb|TestCavlc|TestWelsMotionEstimateIntegerFast|TestMeRefineFracPixelFast'],{}),
 ('vet',['go','vet','./pkg/h264/...','./internal/rdp/...'],{}),
 ('386',['go','build','./pkg/h264/...','./internal/rdp/...'],{'GOARCH':'386','GOOS':'linux'}),
]
for package in ['common','encoder']:
 checks.append(('arm64-'+package,['go','test','-c','-o',str(root/f'final-{package}-arm64.test'),f'./pkg/h264/internal/{package}'],{'GOARCH':'arm64','GOOS':'linux'}))
checks.append(('arm64-avc',['go','test','-c','-o',str(root/'final-avc-arm64.test'),'./internal/rdp/avc'],{'GOARCH':'arm64','GOOS':'linux'}))
records=[]
for name,cmd,extra in checks:
 started=time.monotonic()
 with (out/f'{name}.log').open('w') as log:
  done=subprocess.run(cmd,cwd=source,env=env|extra,stdout=log,stderr=subprocess.STDOUT)
 record={'name':name,'command':cmd,'environment':extra,'exit_code':done.returncode,'seconds':time.monotonic()-started}
 records.append(record);(out/'checks.json').write_text(json.dumps(records,indent=2)+'\n')
 print(name,done.returncode,round(record['seconds'],2),flush=True)
if any(r['exit_code'] for r in records):raise SystemExit(1)
