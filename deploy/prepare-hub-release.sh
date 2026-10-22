#!/usr/bin/env bash
# Package existing API/updater builds plus a credential-free public frontend.
# This script builds one complete installer image and never pushes or installs it.
set -euo pipefail
umask 077
version=${1:?usage: prepare-hub-release.sh VERSION [DOCKER_REPOSITORY]}
repo=${2:-chenrui9814/voex}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
[[ "$repo" =~ ^[a-z0-9][a-z0-9._/-]+$ ]] || { echo 'Invalid Docker repository' >&2; exit 1; }
backend=$(cd "$(dirname "$0")/.." && pwd)
frontend=${VOEX_FRONTEND_DIR:-/home/voex}
artifacts="$backend/deploy/artifacts"
stage="$artifacts/hub-release"
web="$artifacts/hub-web"
api="voex/api:$version"
updater="voex/updater:$version"
nginx=${NGINX_IMAGE:-nginx:latest}
mysql=${MYSQL_IMAGE:-mysql:8.0}
for image in "$api" "$updater" "$nginx" "$mysql"; do
 if ! platform=$(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}' 2>/dev/null); then
  printf 'Missing image: %s. Run ./deploy/build-install-image.sh %s %s to build from source and export the complete installer.\n' "$image" "$version" "$repo" >&2
  exit 1
 fi
 [[ "$platform" == linux/amd64 ]] || { printf 'Unsupported platform for %s: %s; expected linux/amd64.\n' "$image" "$platform" >&2; exit 1; }
done
installer_base=$(docker image inspect "$nginx" --format '{{.Id}}')
[[ -x "$artifacts/voex-release" && -f "$backend/.release-keys/signing.key" && -f "$backend/.release-keys/release.pub" ]]
# Never change the private .env files or the deployed frontend.
(cd "$frontend" && VITE_PEXELS_API_KEY= NODE_OPTIONS=--max-old-space-size=1024 RAYON_NUM_THREADS=2 pnpm exec vite build --outDir "$web" --emptyOutDir)
python3 - "$frontend" "$web" "$version" <<'PY'
import os,re,sys
from pathlib import Path
source,web,version=Path(sys.argv[1]),Path(sys.argv[2]),sys.argv[3]
values={}
for name in ('.env','.env.local','.env.production','.env.production.local'):
 path=source/name
 if not path.is_file():continue
 for line in path.read_text().splitlines():
  match=re.match(r'\s*(?:export\s+)?(VITE_[A-Za-z0-9_]+)\s*=\s*(.*)',line)
  if not match:continue
  key,value=match.groups()
  if not re.search(r'KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL',key,re.I):continue
  value=value.strip()
  if value[:1] in ('"',"'") and value[-1:]==value[:1]:value=value[1:-1]
  else:value=value.split(' #',1)[0].strip()
  if value:values.setdefault(key,set()).add(value.encode())
for key,value in os.environ.items():
 if key.startswith('VITE_') and re.search(r'KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL',key,re.I) and value:
  values.setdefault(key,set()).add(value.encode())
for path in web.rglob('*'):
 if not path.is_file():continue
 data=path.read_bytes()
 for key,secrets in values.items():
  if any(value in data for value in secrets):raise SystemExit('Public build contains a credential value: '+key)
(web/'version.json').write_text('{"version":"'+version+'"}\n')
print('Public frontend credential-value scan passed.')
PY
find "$web" -type d -exec chmod 755 {} +
find "$web" -type f -exec chmod 644 {} +
mkdir -p "$stage"
# Refuse unexpected leftovers rather than accidentally publishing them.
python3 - "$stage" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1])
allowed={'frontend.tar.gz','images.tar','compose.yaml','nginx.conf.template','install-hub.sh','voex-release','release.pub','hub.json','hub.sig','LICENSE'}
if any(f.name not in allowed or f.is_symlink() or not f.is_file() for f in p.iterdir()):raise SystemExit('Unexpected staging file; review before packaging.')
PY
cp "$backend/deploy/compose.yaml" "$backend/deploy/nginx.conf.template" "$backend/deploy/install-hub.sh" "$stage/"
cp "$artifacts/voex-release" "$backend/.release-keys/release.pub" "$backend/LICENSE" "$stage/"
chmod 755 "$stage/install-hub.sh" "$stage/voex-release"
tar --sort=name -czf "$stage/frontend.tar.gz" -C "$web" .
docker save --output "$stage/images.tar" "$api" "$updater" "$nginx" "$mysql"
python3 - "$stage" "$version" "$api" "$updater" "$nginx" "$mysql" "$backend" <<'PY'
import hashlib,json,re,subprocess,sys
from pathlib import Path
stage,version,api,updater,nginx,mysql,backend=sys.argv[1:]
p=Path(stage)
def sha(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for c in iter(lambda:f.read(1024*1024),b''):h.update(c)
 return h.hexdigest()
source='\n'.join(f.read_text() for f in (Path(backend)/'internal/database').glob('*.go'))
schema=re.search(r'LatestVersion\s*=\s*"([^"]+)"',source).group(1)
m={'format':'voex-hub-install-v2','version':version,'schemaVersion':schema}
for role,image in [('api',api),('updater',updater),('nginx',nginx),('mysql',mysql)]:
 m[role+'Image']=image
 m[role+'ImageID']=subprocess.check_output(['docker','image','inspect',image,'--format','{{.Id}}'],text=True).strip()
m.update(frontendSHA256=sha(p/'frontend.tar.gz'),imagesSHA256=sha(p/'images.tar'),files={n:sha(p/n) for n in ('compose.yaml','nginx.conf.template','install-hub.sh')})
(p/'hub.json').write_text(json.dumps(m,indent=2)+'\n')
PY
"$artifacts/voex-release" sign "$backend/.release-keys/signing.key" "$stage/hub.json" "$stage/hub.sig"
"$artifacts/voex-release" verify "$stage/release.pub" "$stage/hub.json" "$stage/hub.sig"
docker build --build-arg "INSTALLER_BASE=$installer_base" -f "$backend/deploy/Dockerfile.install" -t "$repo:install-$version" "$backend/deploy"
printf 'Built complete installer: %s:install-%s\nNo installation or registry push performed.\n' "$repo" "$version"
