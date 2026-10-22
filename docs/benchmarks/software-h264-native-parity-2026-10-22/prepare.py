#!/usr/bin/env python3
"""Decode the local reference streams outside benchmark timing."""
import hashlib
import json
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parent
SRC=Path('/home/define42/git/openh264/res')
steps=[
 ('VID_1920x1080_cabac_temporal_direct.264','road1080.i420',1920,1080,'yuv420p'),
 ('VID_1920x1080_cabac_temporal_direct.264','road720.i420',1280,720,'yuv420p'),
 ('VID_1920x1080_cabac_temporal_direct.264','road1080.rgba',1920,1080,'rgba'),
 ('Adobe_PDF_sample_a_1024x768_50Frms.264','pdf1080.i420',1920,1080,'yuv420p'),
]
manifest=[]
for source,destination,width,height,fmt in steps:
    target=ROOT/destination
    cmd=['ffmpeg','-v','error','-xerror','-threads','1','-i',str(SRC/source),'-vf',f'scale={width}:{height}:flags=bilinear','-threads','1','-pix_fmt',fmt,'-fps_mode','passthrough','-f','rawvideo','-y',str(target)]
    subprocess.run(cmd,check=True)
    data=target.read_bytes()
    size=width*height*(4 if fmt=='rgba' else 1.5)
    assert len(data)%size==0
    item=dict(source=source,destination=destination,width=width,height=height,format=fmt,frames=int(len(data)/size),bytes=len(data),sha256=hashlib.sha256(data).hexdigest(),command=cmd)
    manifest.append(item)
    print(item,flush=True)
(ROOT/'fixtures.json').write_text(json.dumps(manifest,indent=2)+'\n')
