#!/usr/bin/env bash
# One-time v1 -> v2 infrastructure transition. Application/database version is preserved.
set -euo pipefail
umask 077
here=$(cd "$(dirname "$0")" && pwd)
python3 - "$here" "${1:-/home/voex-deploy}" <<'PY'
import datetime,hashlib,json,os,pathlib,re,shutil,subprocess,sys,tarfile,tempfile,time,urllib.request
package=pathlib.Path(sys.argv[1]).resolve();deploy=pathlib.Path(sys.argv[2]).resolve()
if not deploy.is_absolute() or deploy==pathlib.Path('/'):raise SystemExit('Invalid deployment directory')
runtime=deploy/'runtime.env'
if not runtime.is_file():raise SystemExit('Existing deployment not found')
original=runtime.read_bytes()
def values(raw):
 result={}
 for line in raw.decode().splitlines():
  if not line.strip() or line.lstrip().startswith('#'):continue
  k,v=line.split('=',1);result[k.strip()]=v.strip().strip('"').strip("'")
 return result
env=values(original)
if 'VOEX_NGINX_IMAGE' in env:
 print('Fixed Nginx layout is already installed.');sys.exit(0)
version=env.get('VOEX_VERSION','')
if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?',version) or not env.get('VOEX_WEB_IMAGE'):
 raise SystemExit('Expected a known v1 deployment')
marker=deploy/'state/maintenance'
if marker.exists():raise SystemExit('Deployment is in maintenance; finish recovery first')
def assert_idle():
 for path in (deploy/'jobs').glob('*.json'):
  if json.loads(path.read_text())['job']['status'] in ('queued','running'):
   raise RuntimeError('An upgrade is still in progress')
assert_idle()
cli=str(package/'voex-release')
for name in ('release','bootstrap'):
 subprocess.run([cli,'verify',str(deploy/'release.pub'),str(package/(name+'.json')),str(package/(name+'.sig'))],check=True)
release=json.loads((package/'release.json').read_text());bootstrap=json.loads((package/'bootstrap.json').read_text())
if release.get('formatVersion')!=2 or version not in release['fromVersions']:
 raise SystemExit('This signed package does not support the installed version')
expected={**bootstrap['files'],'images.tar':release['imagesSHA256'],'frontend.tar.gz':release['frontendSHA256']}
for name,wanted in expected.items():
 h=hashlib.sha256()
 with (package/name).open('rb') as f:
  for data in iter(lambda:f.read(1024*1024),b''):h.update(data)
 if h.hexdigest()!=wanted:raise SystemExit('Package checksum mismatch: '+name)
subprocess.run(['docker','load','-i',str(package/'infrastructure.tar')],check=True)
for name in ('nginx','updater'):
 actual=subprocess.check_output(['docker','image','inspect',bootstrap[name+'Image'],'--format','{{.Id}}'],text=True).strip()
 if actual!=bootstrap[name+'ImageID']:raise SystemExit('Infrastructure image mismatch')
backup=deploy/'infrastructure-backups'/datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
backup.mkdir(parents=True,mode=0o700)
shutil.copy2(runtime,backup/'runtime.env');shutil.copy2(deploy/'compose.yaml',backup/'compose.yaml')
if (deploy/'nginx.conf.template').exists():shutil.copy2(deploy/'nginx.conf.template',backup/'nginx.conf.template')
compose=['docker','compose','--env-file',str(runtime),'-f',str(deploy/'compose.yaml')]
def run(*args,capture=False):
 return subprocess.run(compose+list(args),check=True,text=True,stdout=subprocess.PIPE if capture else None).stdout
web_id=run('ps','-q','web',capture=True).strip()
if not web_id:raise SystemExit('Existing frontend container is missing')
port=int(env.get('VOEX_HTTP_PORT','18080'))
def healthy():
 try:
  for path in ['/api/health/ready','/app/version.json']:
   with urllib.request.urlopen(f'http://127.0.0.1:{port}'+path,timeout=5) as response:
    if json.load(response).get('version')!=version:return False
  return True
 except Exception:return False
moved=[];changed=False;marker_owned=False
try:
 marker.write_text('frontend layout migration\n');marker.chmod(0o644);marker_owned=True
 run('stop','-t','40','api')
 try:assert_idle()
 except RuntimeError:
  # An already accepted job owns recovery; do not stop its updater or replace config.
  raise SystemExit('An upgrade was accepted concurrently; let it finish before retrying this transition.')
 run('stop','updater')
 with tempfile.TemporaryDirectory(prefix='frontend-bootstrap-',dir=backup) as temporary:
  temporary=pathlib.Path(temporary);dist=temporary/'dist';dist.mkdir()
  subprocess.run(['docker','cp',web_id+':/usr/share/nginx/html/app/.',str(dist)],check=True)
  if json.loads((dist/'version.json').read_text()).get('version')!=version:raise RuntimeError('Installed frontend version mismatch')
  archive=temporary/'frontend.tar.gz'
  with tarfile.open(archive,'w:gz') as tar:
   for file in sorted(dist.rglob('*')):tar.add(file,arcname=str(file.relative_to(dist)),recursive=False)
  subprocess.run([cli,'frontend-install',str(archive),str(deploy/'frontend'),version],check=True)
  subprocess.run([cli,'frontend-switch',str(deploy/'frontend'),version],check=True)
 legacy=deploy/'legacy-releases';legacy.mkdir(exist_ok=True,mode=0o700)
 for path in (deploy/'releases').iterdir():
  if not path.is_dir():continue
  if json.loads((path/'release.json').read_text()).get('formatVersion')==2:continue
  if (legacy/path.name).exists():raise RuntimeError('Legacy archive destination already exists')
  destination=legacy/path.name;path.rename(destination);moved.append((path,destination))
 shutil.copy2(package/'compose.yaml',deploy/'compose.yaml')
 shutil.copy2(package/'nginx.conf.template',deploy/'nginx.conf.template')
 updates={'VOEX_NGINX_IMAGE':bootstrap['nginxImageID'],'VOEX_UPDATER_IMAGE':bootstrap['updaterImageID']}
 lines=[]
 for line in original.decode().splitlines():
  key=line.split('=',1)[0].strip()
  if key=='VOEX_WEB_IMAGE':continue
  lines.append(key+'='+updates.pop(key) if key in updates else line)
 lines += [k+'='+v for k,v in updates.items()]
 new=deploy/'runtime.env.next';new.write_text('\n'.join(lines)+'\n');new.chmod(0o600);new.replace(runtime);changed=True
 run('config','--quiet')
 run('up','-d','--no-deps','updater','web','api')
 for _ in range(60):
  if healthy():break
  time.sleep(2)
 else:raise RuntimeError('New frontend layout failed readiness')
 marker.unlink();marker_owned=False
 print('Fixed Nginx layout installed; application version preserved:',version)
 print('Infrastructure backup:',backup)
except Exception:
 # Application/DB migrations have not run; restoring the infrastructure is sufficient.
 try:
  run('stop','-t','40','api','updater','web')
  shutil.copy2(backup/'runtime.env',runtime);shutil.copy2(backup/'compose.yaml',deploy/'compose.yaml')
  for original_path,archived_path in reversed(moved):archived_path.rename(original_path)
  if (backup/'nginx.conf.template').exists():shutil.copy2(backup/'nginx.conf.template',deploy/'nginx.conf.template')
  run('up','-d','--no-deps','api','updater','web')
  for _ in range(60):
   if healthy():break
   time.sleep(2)
  else:raise RuntimeError('Original deployment recovery failed')
  if marker_owned:marker.unlink()
 except Exception:
  print('Recovery incomplete; maintenance retained. Inspect:',backup,file=sys.stderr)
 raise
PY
