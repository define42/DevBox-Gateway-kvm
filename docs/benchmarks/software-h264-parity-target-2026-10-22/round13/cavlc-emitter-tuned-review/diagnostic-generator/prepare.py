#!/usr/bin/env python3
"""Prepare a diagnostic-only source copy of either baseline or tuned candidate.
No builds, runs, or timing; never use the instrumented build for benchmarks.
"""
from pathlib import Path
import argparse
import shutil
parser=argparse.ArgumentParser()
parser.add_argument('--source',type=Path,required=True)
parser.add_argument('--output',type=Path,required=True)
args=parser.parse_args()
if args.output.exists(): raise SystemExit('output already exists')
shutil.copytree(args.source,args.output)
p=args.output/'pkg/h264/internal/encoder'
# Candidate test mirror must not redeclare the new diagnostic production helper.
(p/'cavlc_emit_eligibility_test.go').unlink(missing_ok=True)
here=Path(__file__).resolve().parent
for src,dst in [('eligibility.go.txt','z_cavlc_emitter_eligibility.go'),('lifecycle.go.txt','z_cavlc_emitter_diagnostic.go')]:
 (p/dst).write_text((here/src).read_text())
f=p/'set_mb_syn_cavlc.go';s=f.read_text()
a='\tif iCalRunLevelFlag == 0 {\n'
assert s.count(a)==1
s=s.replace(a,a+'\t\tcavlcEmitterDiagRecord(nil, nil, nil, pBs, 0, 0, 0, iEndIdx, iResidualProperty)\n')
a='\t// Extraction is completed exactly once:' if '\t// Extraction is completed exactly once:' in s else '\tif iTotalCoeffs > 3 {'
assert s.count(a)==1
s=s.replace(a,'\tcavlcEmitterDiagRecord(&bs, &iLevel, &uiRun, pBs, iCalRunLevelFlag, iTotalCoeffs, iTotalZeros, iEndIdx, iResidualProperty)\n'+a)
f.write_text(s)
f=p/'welsEncoderExt.go';s=f.read_text()
a='func (p *CWelsH264SVCEncoder) EncodeFrame(kpSrcPic *api.SSourcePicture, pBsInfo *api.SFrameBSInfo) (result int32) {\n'
assert s.count(a)==1
s=s.replace(a,a+'\tcavlcEmitterDiagBegin(p)\n')
a='func (p *CWelsH264SVCEncoder) Uninitialize() int32 {\n\tif !p.m_bInitialFlag {\n\t\treturn 0\n\t}\n'
assert s.count(a)==1
s=s.replace(a,a+'\tcavlcEmitterDiagFinish(p)\n')
f.write_text(s)
print(args.output)
