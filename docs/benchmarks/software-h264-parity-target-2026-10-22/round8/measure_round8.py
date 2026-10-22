from pathlib import Path
import os,subprocess,shutil,json,hashlib
r=Path(__file__).resolve().parent;repo=Path('/home/define42/git/DevBox-Gateway-kvm')
env=os.environ|{'PATH':'/snap/go/current/bin:'+os.environ['PATH'],'CGO_ENABLED':'0','GOMAXPROCS':'4'}
print('combined validation',flush=True)
subprocess.run(['python3',str(r/'validate.py')],env=env,check=True)
subprocess.run(['git','diff','--check'],cwd=repo,check=True)
p=r/'round8';p.mkdir(exist_ok=True)
for name in ['pkg','internal']:shutil.copytree(repo/name,p/name,dirs_exist_ok=True)
for name in ['go.mod','go.sum']:shutil.copy2(repo/name,p/name)
(r/'round8-sources.sha256').write_text(''.join(hashlib.sha256(f.read_bytes()).hexdigest()+'  '+str(f.relative_to(p))+'\n' for f in sorted((p/'pkg/h264').rglob('*')) if f.is_file()))
overlay={'Replace':{str(p/'internal/rdp/avc/z_openh264_comparison_test.go'):str(r/'compare_test.go'),str(p/'internal/rdp/avc/z_fullscreen_review_test.go'):str(r/'fullscreen_test.go')}}
(r/'round8-overlay.json').write_text(json.dumps(overlay,indent=2)+'\n')
assert (r/'round7-avc.test').exists(), 'Preserve frozen round7 baseline'
subprocess.run(['go','test','-c','-overlay='+str(r/'round8-overlay.json'),'-o',str(r/'after-avc.test'),'./internal/rdp/avc'],cwd=p,env=env,check=True)
print('round8 stream preflight',flush=True)
subprocess.run(['python3',str(r/'measure.py'),'--preflight','--after-only'],env=env,check=True)
print('round8 native comparison',flush=True)
subprocess.run(['python3',str(r/'measure.py'),'--baseline-binary',str(r/'round7-avc.test')],env=env,check=True)
for label,kinds in [('codec-benchstat',['before','after','native']),('codec-native-benchstat',['after','native'])]:
 with (r/(label+'.txt')).open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',*(str(r/'measure'/f'{kind}.txt') for kind in kinds)],stdout=f,check=True)
subprocess.run(['python3',str(r/'summarize_codec.py')],env=env,check=True)
print('full EncodeDamage comparison',flush=True)
subprocess.run(['python3',str(r/'measure_full.py')],env=env,check=True)
with (r/'full-benchstat.txt').open('w') as f:subprocess.run(['/home/define42/go/bin/benchstat',str(r/'full/before.txt'),str(r/'full/after.txt')],stdout=f,check=True)
subprocess.run(['python3',str(r/'summarize_full.py')],env=env,check=True)
print('round8 comparison complete',flush=True)
