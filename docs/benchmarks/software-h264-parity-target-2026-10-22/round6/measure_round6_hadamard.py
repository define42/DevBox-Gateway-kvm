from pathlib import Path
import os,subprocess,shutil
r=Path(__file__).resolve().parent;e=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
for name in ['hadamard_quant_bench_test.go','hadamard_chroma_bench_test.go']:
 shutil.copy2(r/'hadamard-next/pkg/h264/internal/encoder'/name,r/'component-round6-base/pkg/h264/internal/encoder'/name)
shutil.copy2(r/'hadamard-baseline-shim.go.txt',r/'component-round6-base/pkg/h264/internal/encoder/hadamard_baseline_shim_test.go')
for kind in ['component-round6-base','hadamard-next']:
 print('test/build '+kind,flush=True)
 with (r/(kind+'-hadamard-tests.txt')).open('w') as f:subprocess.run(['go','test','./pkg/h264/internal/encoder','-count=1'],cwd=r/kind,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 subprocess.run(['go','test','-c','-o',str(r/(kind+'-encoder.test')),'./pkg/h264/internal/encoder'],cwd=r/kind,env=e,check=True)
 (r/('hadamard-next-'+kind+'.txt')).write_text('')
subprocess.run(['python3',str(r/'measure_round6_extra.py'),'hadamard-next','encoder','^Benchmark(HadamardQuant2x2|ChromaCoefficientEncoding)$'],env=e,check=True)
