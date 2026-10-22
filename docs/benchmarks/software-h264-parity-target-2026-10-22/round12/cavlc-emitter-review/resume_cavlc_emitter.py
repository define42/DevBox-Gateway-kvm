#!/usr/bin/env python3
"""Resume after an unused test import; retain initial failed build and all evidence."""
import importlib.util,json,os,re,shutil,traceback
from pathlib import Path
r=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('emitter',r/'measure_cavlc_emitter.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
out=m.OUT
initial=json.loads((out/'completion.json').read_text())
assert initial['failed_stage']=='after-full-encoder-tests' and not initial['complete']
assert not (out/'initial-failed-completion.json').exists()
shutil.copy2(out/'completion.json',out/'initial-failed-completion.json')
shutil.copy2(__file__,out/Path(__file__).name)
p=m.AFTER/m.PACKAGE/'cavlc_emit_mb_test.go'
old=p.read_text();needle='\n\t"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"\n'
assert old.count(needle)==1 and 'common.' not in old
before=m.sha(p);p.write_text(old.replace(needle,''))
env=os.environ.copy()
for key in ('GODEBUG','GOFLAGS','GOOS','GOARCH','CAVLC_NATIVE_BINARY','CAVLC_NATIVE_FIXTURES'):env.pop(key,None)
env.update(PATH='/snap/go/current/bin:'+os.environ['PATH'],CGO_ENABLED='0',GOMAXPROCS='4',CAVLC_NATIVE_BINARY=str(m.NATIVE),CAVLC_NATIVE_FIXTURES=str(out/'correctness-fixtures447.txt'))
m.STATUS=initial.copy();m.STATUS.pop('failed_stage');m.STATUS.pop('error');m.STATUS['initial_failure']='initial-failed-completion.json'
try:
    m.run('after-unused-import-gofmt',[m.GOFMT,'-w',str(p)],m.AFTER,env)
    m.write_json(out/'test-import-correction.json',{'path':str(p),'before_sha256':before,'after_sha256':m.sha(p),'change':'Remove unused common import from test only; production unchanged','original_failure':'after-full-encoder-tests.txt'})
    shutil.copy2(out/'after-postformat-source-sha256.json',out/'after-initial-postformat-source-sha256.json')
    m.write_json(out/'after-postformat-source-sha256.json',m.source_hashes(m.AFTER))
    log=m.run('after-corrected-full-encoder-tests',[m.GO,'test','-json','./'+str(m.PACKAGE),'-count=1'],m.AFTER,env)
    required=['TestCavlcNativeFixtures','TestCavlcNativeFullParity','TestCavlcNativeFullParity/auto','TestCavlcNativeFullParity/scalar']
    passed=m.require_pass(log,required)
    m.write_json(out/'after-native447-validation.json',{'required_passed':required,'fixture_sha256':m.sha(out/'correctness-fixtures447.txt'),'suites':447,'all_passed_tests':passed,'native_binary_sha256':m.sha(m.NATIVE)})
    m.run('after-emitter-race',[m.GO,'test','-race','./'+str(m.PACKAGE),'-run=^TestCavlcEmitter','-count=1'],m.AFTER,env|{'CGO_ENABLED':'1'})
    m.run('after-emitter-cpu-all-off',[m.GO,'test','./'+str(m.PACKAGE),'-run=^TestCavlcEmitter','-count=1'],m.AFTER,env|{'GODEBUG':'cpu.all=off','GOMAXPROCS':'1'})
    m.run('after-build',[m.GO,'test','-c','-o',str(out/'after.test'),'./'+str(m.PACKAGE)],m.AFTER,env)
    m.run('after-build-info',[m.GO,'version','-m',str(out/'after.test')],m.AFTER,env)
    m.write_json(out/'after-binary-sha256.json',{'binary':str(out/'after.test'),'sha256':m.sha(out/'after.test')})
    (out/'after.txt').write_text('')
    timing=env|{'GOMAXPROCS':'1','CAVLC_NATIVE_FIXTURES':str(out/'timing-fixtures333.txt')};timing.pop('CAVLC_NATIVE_BINARY')
    m.write_json(out/'timing-environment.json',m.env_record(timing))
    expected={}
    for rep in range(6):
        for kind in (['base','after'] if rep%2==0 else ['after','base']):
            tree=m.BASE if kind=='base' else m.AFTER
            for group,pattern,count in m.GROUPS:
                name=f'pair{rep+1:02d}-{kind}-{group}'
                log=m.run(name,['taskset','-c','2',str(out/(kind+'.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchtime=200ms','-test.benchmem','-test.count=1'],tree,timing)
                text=log.read_text();names=re.findall(r'^(Benchmark\S+)\s+\d+\s+',text,re.MULTILINE)
                assert len(names)==count and len(set(names))==count,(name,len(names))
                assert group not in expected or set(names)==expected[group],name
                expected[group]=set(names)
                with (out/(kind+'.txt')).open('a') as f:f.write(text)
        print(f'CAVLC emitter pair {rep+1}/6 complete',flush=True)
    m.run('benchstat',[m.BENCHSTAT,str(out/'base.txt'),str(out/'after.txt')],out,timing)
    for kind,tree in [('base',m.BASE),('after',m.AFTER)]:assert m.source_hashes(tree)==json.loads((out/(kind+'-postformat-source-sha256.json')).read_text())
    m.STATUS.update(complete=True,finished_utc=m.utc(),pairs_completed=6,matched_cases_per_pair=67,native_timing_runs=0,result=str(out/'benchstat.txt'))
    m.write_json(out/'completion.json',m.STATUS)
except BaseException as e:
    m.STATUS.update(complete=False,failed_stage=m.STAGE,finished_utc=m.utc(),error=str(e))
    m.write_json(out/'completion.json',m.STATUS)
    (out/'resume-failure.txt').write_text(traceback.format_exc())
    raise
