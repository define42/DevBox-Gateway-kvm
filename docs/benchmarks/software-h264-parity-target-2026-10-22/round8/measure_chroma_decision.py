from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent;out=r/'chroma-decision-review';out.mkdir(exist_ok=True)
base=r/'chroma-decision-round7-base'
if not base.exists():shutil.copytree(r/'round7',base)
for name in ['chroma_decision_test.go','chroma_decision_reference_test.go']:
 shutil.copy2(r/'chroma-decision-next/pkg/h264/internal/encoder'/name,base/'pkg/h264/internal/encoder'/name)
e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for kind,src in [('base',base),('after',r/'chroma-decision-next')]:
 print('test/build '+kind,flush=True)
 with (out/(kind+'-tests.txt')).open('w') as f:
  subprocess.run(['go','test','./pkg/h264/internal/encoder','-run=Test(ChromaDecision|CoefficientEncodingStateParity)','-count=1'],cwd=src,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 subprocess.run(['go','test','-c','-o',str(out/(kind+'.test')),'./pkg/h264/internal/encoder'],cwd=src,env=e,check=True)
 (out/(kind+'.txt')).write_text('')
for rep in range(6):
 for kind in (['base','after'] if rep%2==0 else ['after','base']):
  p=subprocess.run(['taskset','-c','2',str(out/(kind+'.test')),'-test.run=^$','-test.bench=^BenchmarkChromaCoefficientEncoding$','-test.benchtime=250ms','-test.benchmem'],env=e,capture_output=True,text=True,check=True)
  with (out/(kind+'.txt')).open('a') as f:f.write(p.stdout)
 print('chroma decision pair '+str(rep+1),flush=True)
with (out/'benchstat.txt').open('w') as f:
 subprocess.run(['/home/define42/go/bin/benchstat',str(out/'base.txt'),str(out/'after.txt')],stdout=f,check=True)
