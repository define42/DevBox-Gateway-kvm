from pathlib import Path
import os,subprocess,hashlib
base=Path(__file__).resolve().parent
lines=[]
for slices in (1,4):
    streams=[]
    for label in ('native','go'):
        stream=base/f'{label}-{slices}.264'
        env=dict(os.environ,GOMAXPROCS='4',COMPARE_RUN='1',COMPARE_I420=str(base/'clip1080p.i420'),COMPARE_FRAMES='120',COMPARE_SLICES=str(slices),COMPARE_STREAM=str(stream))
        cmd=([str(base/'go.test'),'-test.run=^TestOpenH264Comparison$','-test.v'] if label=='go' else [str(base/'native_bench'),str(base/'clip1080p.i420'),str(slices),'120',str(stream)])
        result=subprocess.run(cmd,env=env,text=True,capture_output=True,check=True)
        (base/f'preflight-{label}-{slices}.log').write_text(result.stdout+result.stderr)
        data=stream.read_bytes()
        lines.append(f'{label} slices={slices} bytes={len(data)} sha256={hashlib.sha256(data).hexdigest()}')
        streams.append(data)
    lines.append(f'slices={slices} bitstreams_identical={streams[0]==streams[1]}')
(base/'parity.txt').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines))
