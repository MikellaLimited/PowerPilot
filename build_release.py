#!/usr/bin/env python3
from __future__ import annotations
import hashlib, json, os, re, shutil, subprocess, sys, zipfile
from pathlib import Path

ROOT=Path(__file__).resolve().parent
OUT=ROOT/'release'
GOENV={**os.environ,'GOOS':'windows','GOARCH':'amd64','CGO_ENABLED':'0','GO111MODULE':'off'}
LDFLAGS='-s -w -H windowsgui'

def run(args,cwd=None):
    print('+',' '.join(map(str,args)))
    subprocess.run(args,cwd=cwd or ROOT,env=GOENV,check=True)

def build(pkg:Path,out:Path):
    run(['go','test','./...'],cwd=pkg)
    run(['go','build','-trimpath','-ldflags',LDFLAGS,'-o',str(out),'.'],cwd=pkg)

def sha(p:Path)->str:
    h=hashlib.sha256()
    with p.open('rb') as f:
        for b in iter(lambda:f.read(1<<20),b''):h.update(b)
    return h.hexdigest()

def version()->str:
    s=(ROOT/'app/main.go').read_text(encoding='utf-8')
    m=re.search(r'const\s+appVersion\s*=\s*"([^"]+)"',s)
    if not m: raise SystemExit('appVersion not found')
    return m.group(1)

def make_update_zip(ver:str, app:Path, updater:Path, out:Path):
    manifest={"version":ver,"files":[{"source":"PowerPilot.exe","role":"app","sha256":"sha256:"+sha(app)}]}
    with zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
        z.write(app,'PowerPilot.exe')
        z.write(updater,'PowerPilot.Update.exe')
        z.writestr('update_manifest.json',json.dumps(manifest,ensure_ascii=False,indent=2))

def make_source_zip(ver:str,out:Path):
    prefix=f'PowerPilot_{ver}_source/'
    skip_roots={'.git','.tools','.venv','release'}
    with zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
        for p in sorted(ROOT.rglob('*')):
            if not p.is_file(): continue
            rel=p.relative_to(ROOT)
            if rel.parts and rel.parts[0] in skip_roots: continue
            if rel.as_posix().startswith('installer/payload/') and p.suffix.lower()=='.exe': continue
            if p.suffix.lower() in {'.exe','.test'}: continue
            if '__pycache__' in rel.parts: continue
            z.write(p,prefix+rel.as_posix())

def main():
    ver=version()
    if OUT.exists(): shutil.rmtree(OUT)
    OUT.mkdir(parents=True)
    tmp=OUT/'_tmp'; tmp.mkdir()
    icon=ROOT/'app/assets/PowerPilot.ico'
    build(ROOT/'app',tmp/'PowerPilot.raw.exe')
    run([sys.executable,str(ROOT/'patch_pe_icon.py'),str(tmp/'PowerPilot.raw.exe'),str(icon),str(OUT/'PowerPilot.exe')])
    build(ROOT/'updater',OUT/'PowerPilot.Update.exe')
    build(ROOT/'uninstaller',tmp/'Uninstall.raw.exe')
    run([sys.executable,str(ROOT/'patch_pe_icon.py'),str(tmp/'Uninstall.raw.exe'),str(ROOT/'uninstaller/PowerPilot.ico'),str(OUT/'Uninstall.exe')])
    payload=ROOT/'installer/payload'; payload.mkdir(parents=True,exist_ok=True)
    shutil.copy2(OUT/'PowerPilot.exe',payload/'PowerPilot.exe')
    shutil.copy2(OUT/'Uninstall.exe',payload/'Uninstall.exe')
    shutil.copy2(icon,payload/'PowerPilot.ico')
    build(ROOT/'installer',tmp/'Setup.raw.exe')
    run([sys.executable,str(ROOT/'patch_pe_icon.py'),str(tmp/'Setup.raw.exe'),str(ROOT/'installer/assets/PowerPilot.ico'),str(OUT/'PowerPilot_Setup.exe')])
    if sha(OUT/'PowerPilot.exe') != sha(payload/'PowerPilot.exe'): raise SystemExit('installer payload mismatch')
    run([sys.executable,str(ROOT/'release_checks.py'),str(OUT),str(icon)])

    shutil.copy2(OUT/'PowerPilot_Setup.exe',OUT/f'PowerPilot_Setup_{ver}.exe')
    shutil.copy2(OUT/'PowerPilot.exe',OUT/f'PowerPilot_Portable_{ver}.exe')
    make_update_zip(ver,OUT/'PowerPilot.exe',OUT/'PowerPilot.Update.exe',OUT/f'PowerPilot_Update_{ver}.zip')
    make_source_zip(ver,OUT/f'PowerPilot_{ver}_source.zip')
    notes=ROOT/'RELEASE_NOTES.txt'
    if notes.exists(): shutil.copy2(notes,OUT/f'PowerPilot_{ver}_RELEASE_NOTES.txt')
    validation=OUT/'release_validation.json'
    if validation.exists():
        report=json.loads(validation.read_text(encoding='utf-8'))
        artifacts={}
        for a in [OUT/f'PowerPilot_Setup_{ver}.exe', OUT/f'PowerPilot_Portable_{ver}.exe', OUT/f'PowerPilot_Update_{ver}.zip', OUT/f'PowerPilot_{ver}_source.zip', OUT/f'PowerPilot_{ver}_RELEASE_NOTES.txt']:
            if a.exists(): artifacts[a.name]={"bytes":a.stat().st_size,"sha256":sha(a)}
        report['version']=ver
        report['release_artifacts']=artifacts
        report['setup_explorer_file_icon_visual_check']='PENDING'
        report['windows_runtime_update_cycle']='NOT RUN'
        validation.write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
        shutil.copy2(validation,OUT/f'PowerPilot_{ver}_release_validation.json')
    # Clean non-user-facing build intermediates from the release directory.
    for p in ['PowerPilot.exe','PowerPilot_Setup.exe','PowerPilot.Update.exe','Uninstall.exe','release_validation.json']:
        try:(OUT/p).unlink()
        except FileNotFoundError:pass
    shutil.rmtree(tmp)
    print('\nRelease ready:',OUT)

if __name__=='__main__': main()
