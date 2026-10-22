import os
from pathlib import Path
import subprocess

out=Path('/tmp/h264-native-parity-20261021')
env=os.environ.copy();env['GOMAXPROCS']='4'
commands={
 'before':[
  [str(out/'before-encoder.test'),'-test.run=^$','-test.bench=BenchmarkSample(SatdBlocks|AvgSatd)|BenchmarkQuarterRefineFused','-test.benchtime=100ms','-test.count=1'],
  [str(out/'before-common.test'),'-test.run=^$','-test.bench=^BenchmarkMcAVX2Halfpel$/horizontal/16x16/AVX2$','-test.benchtime=100ms','-test.count=1'],
 ],
 'after':[
  [str(out/'motion-encoder.test'),'-test.run=^$','-test.bench=BenchmarkSample(SatdBlocks|AvgSatd)|BenchmarkQuarterRefineFused','-test.benchtime=100ms','-test.count=1'],
  [str(out/'motion-common.test'),'-test.run=^$','-test.bench=^BenchmarkMcAVX2Halfpel$/horizontal/16x16/AVX2$','-test.benchtime=100ms','-test.count=1'],
 ],
 'native':[[str(out/'motion_component'),'3000000']],
}
for variant in commands: (out/f'motion-{variant}.txt').write_text('')
for pair in range(6):
 order=('before','after','native') if pair%2==0 else ('native','after','before')
 for variant in order:
  with (out/f'motion-{variant}.txt').open('a') as target:
   for command in commands[variant]:
    result=subprocess.run(command,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
    target.write(result.stdout)
 print(f'completed motion pair {pair+1}/6',flush=True)
