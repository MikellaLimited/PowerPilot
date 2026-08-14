#!/usr/bin/env python3
from __future__ import annotations
import hashlib, io, json, struct, sys
from pathlib import Path
from PIL import Image

BASELINE = {
    "PowerPilot.exe": 9_200_000,  # 0.6.17 adds native HTTPS update/PawnIO handling; no PowerShell updater
    "Uninstall.exe": 1_979_392,
    "PowerPilot_Setup.exe": 14_200_000,  # payload includes the larger native sensor-updater PowerPilot executable
}
MAX_GROWTH = 1.20


def sha256(p: Path) -> str:
    h=hashlib.sha256()
    with p.open('rb') as f:
        for b in iter(lambda:f.read(1<<20), b''): h.update(b)
    return h.hexdigest()


def pe_info(path: Path):
    d=path.read_bytes()
    if d[:2]!=b'MZ': raise ValueError(f'{path}: no MZ')
    pe=struct.unpack_from('<I',d,0x3c)[0]
    if d[pe:pe+4]!=b'PE\0\0': raise ValueError(f'{path}: no PE')
    ns=struct.unpack_from('<H',d,pe+6)[0]
    ptrsym, numsym=struct.unpack_from('<II',d,pe+12)
    opt_size=struct.unpack_from('<H',d,pe+20)[0]
    opt=pe+24
    sh=opt+opt_size
    sections=[]
    for i in range(ns):
        o=sh+i*40
        name=d[o:o+8].rstrip(b'\0').decode('ascii','replace')
        vs,va,rs,rp=struct.unpack_from('<IIII',d,o+8)
        sections.append((name,vs,va,rs,rp))
    return d,pe,opt,sections,ptrsym,numsym


def rva_to_off(rva, sections):
    for _name,vs,va,rs,rp in sections:
        if va <= rva < va+max(vs,rs):
            return rp+(rva-va)
    raise ValueError(f'RVA {rva:x} not mapped')


def resource_data(d,opt,sections,type_id,res_id=1):
    # PE32+ data directory starts at +112; resources is entry #2.
    rrva,rsize=struct.unpack_from('<II',d,opt+112+16)
    if not rrva or not rsize: raise ValueError('no resource directory')
    base=rva_to_off(rrva,sections)
    def dir_entries(rel):
        off=base+rel
        named,ids=struct.unpack_from('<HH',d,off+12)
        out=[]
        for i in range(named+ids):
            name,target=struct.unpack_from('<II',d,off+16+i*8)
            out.append((name & 0x7fffffff, bool(name&0x80000000), target))
        return out
    def descend(rel,wanted):
        for name,isstr,target in dir_entries(rel):
            if not isstr and name==wanted:
                if not (target&0x80000000): raise ValueError('expected directory')
                return target&0x7fffffff
        raise ValueError(f'resource id {wanted} missing')
    type_rel=descend(0,type_id)
    id_rel=descend(type_rel,res_id)
    # first language
    ents=dir_entries(id_rel)
    if not ents: raise ValueError('resource language missing')
    target=ents[0][2]
    if target&0x80000000: raise ValueError('expected data entry')
    de=base+target
    data_rva,size,_,_=struct.unpack_from('<IIII',d,de)
    return d[rva_to_off(data_rva,sections):rva_to_off(data_rva,sections)+size]


def icon_ids(d,opt,sections):
    grp=resource_data(d,opt,sections,14,1)
    reserved,typ,count=struct.unpack_from('<HHH',grp,0)
    if (reserved,typ)!=(0,1) or count<1: raise ValueError('invalid group icon')
    out=[]
    for i in range(count):
        w,h,_,_,planes,bpp,size,rid=struct.unpack_from('<BBBBHHIH',grp,6+i*14)
        out.append((256 if w==0 else w,256 if h==0 else h,planes,bpp,size,rid))
    return out


def extract_ico(path:Path)->bytes:
    d,pe,opt,sections,_,_=pe_info(path)
    entries=icon_ids(d,opt,sections)
    payloads=[]
    for w,h,planes,bpp,size,rid in entries:
        blob=resource_data(d,opt,sections,3,rid)
        if len(blob)!=size: raise ValueError(f'icon {rid} size mismatch')
        payloads.append(blob)
    head=bytearray(struct.pack('<HHH',0,1,len(entries)))
    offset=6+16*len(entries)
    for (w,h,planes,bpp,size,rid),blob in zip(entries,payloads):
        head += struct.pack('<BBBBHHII',0 if w>=256 else w,0 if h>=256 else h,0,0,planes,bpp,len(blob),offset)
        offset += len(blob)
    return bytes(head)+b''.join(payloads)


def check_icon(exe:Path, source_ico:Path):
    d,pe,opt,sections,_,_=pe_info(exe)
    entries=icon_ids(d,opt,sections)
    sizes=[e[0] for e in entries]
    expected=[16,24,32,48,64,128,256]
    if sizes!=expected: raise ValueError(f'{exe.name}: icon sizes {sizes}, expected {expected}')
    for w,h,planes,bpp,size,rid in entries:
        if planes!=1 or bpp!=32: raise ValueError(f'{exe.name}: {w}px icon planes/bpp={planes}/{bpp}')
    built=Image.open(io.BytesIO(extract_ico(exe))).convert('RGBA')
    src=Image.open(source_ico).convert('RGBA')
    if built.size!=(256,256): built=built.resize((256,256),Image.Resampling.LANCZOS)
    if src.size!=(256,256): src=src.resize((256,256),Image.Resampling.LANCZOS)
    if built.tobytes()!=src.tobytes(): raise ValueError(f'{exe.name}: extracted 256px icon differs from source')
    return entries


def check_release(release:Path, source_ico:Path):
    names=['PowerPilot.exe','Uninstall.exe','PowerPilot_Setup.exe']
    report={}
    for name in names:
        p=release/name
        d,pe,opt,sections,ptrsym,numsym=pe_info(p)
        debug=[n for n,vs,va,rs,rp in sections if 'debug' in n.lower() or 'zdebug' in n.lower()]
        if debug: raise ValueError(f'{name}: debug sections present: {debug}')
        if numsym!=0: raise ValueError(f'{name}: COFF symbols present: {numsym}')
        size=p.stat().st_size
        base=BASELINE[name]
        if size > int(base*MAX_GROWTH):
            raise ValueError(f'{name}: suspicious growth {size:,} > {int(base*MAX_GROWTH):,} (baseline {base:,})')
        report[name]={"bytes":size,"sha256":sha256(p),"sections":[x[0] for x in sections]}

    updater=release/'PowerPilot.Update.exe'
    if not updater.exists(): raise ValueError('PowerPilot.Update.exe missing')
    ud,upe,uopt,usections,uptr,unumsym=pe_info(updater)
    if unumsym!=0: raise ValueError(f'PowerPilot.Update.exe: COFF symbols present: {unumsym}')
    report['PowerPilot.Update.exe']={"bytes":updater.stat().st_size,"sha256":sha256(updater),"sections":[x[0] for x in usections]}

    entries=check_icon(release/'PowerPilot_Setup.exe',source_ico)
    report['setup_icon']=[{"size":e[0],"planes":e[2],"bpp":e[3]} for e in entries]
    (release/'release_validation.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
    return report

if __name__=='__main__':
    if len(sys.argv)!=3:
        print('usage: release_checks.py RELEASE_DIR SOURCE_ICO',file=sys.stderr); raise SystemExit(2)
    r=check_release(Path(sys.argv[1]),Path(sys.argv[2]))
    print(json.dumps(r,ensure_ascii=False,indent=2))
