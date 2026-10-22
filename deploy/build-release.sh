#!/usr/bin/env bash
set -euo pipefail
umask 077
version=${1:?usage: build-release.sh VERSION [FROM_VERSION]}
previous=${2:-}
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
backend=$(cd "$(dirname "$0")/.." && pwd)
frontend=${VOEX_FRONTEND_DIR:-/home/voex}
staging="$backend/deploy/artifacts"
out="$backend/dist"
mkdir -p "$staging/web" "$out" "$backend/.release-keys"
cd "$backend"
nginx_image=${NGINX_IMAGE:-nginx:latest}
mysql_image=${MYSQL_IMAGE:-mysql:8.0}
# Check prerequisites before compiling; missing images are not architecture errors.
for image in "$nginx_image" "$mysql_image"; do
 if ! platform=$(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}' 2>/dev/null); then
  printf 'Missing base image: %s. Import a trusted offline archive or prepare this image before building.\n' "$image" >&2
  exit 1
 fi
 [[ "$platform" == linux/amd64 ]] || { printf 'Unsupported platform for %s: %s; expected linux/amd64.\n' "$image" "$platform" >&2; exit 1; }
done
# Local image IDs also work after docker load, when RepoDigests can be empty.
nginx_base=${NGINX_BASE:-$(docker image inspect "$nginx_image" --format '{{.Id}}')}
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64
export GOMAXPROCS=${GOMAXPROCS:-2}
go build -trimpath -o "$staging/voex-server" ./cmd/voex-server
go build -trimpath -o "$staging/voex-updater" ./cmd/voex-updater
go build -trimpath -o "$staging/voex-release" ./cmd/voex-release
(cd "$frontend" && NODE_OPTIONS=--max-old-space-size=1024 RAYON_NUM_THREADS=2 pnpm exec vite build --outDir "$staging/web" --emptyOutDir)
printf '{"version":"%s"}\n' "$version" > "$staging/web/version.json"
# Public frontend assets must be readable by the unprivileged Nginx worker.
find "$staging/web" -type d -exec chmod 755 {} +
find "$staging/web" -type f -exec chmod 644 {} +
cp /etc/ssl/certs/ca-certificates.crt "$staging/ca-certificates.crt"
cp "$(command -v docker)" "$staging/docker"
compose_binary=${VOEX_COMPOSE_BINARY:-/usr/libexec/docker/cli-plugins/docker-compose}
cp "$compose_binary" "$staging/docker-compose"
api_image="voex/api:$version"
updater_image="voex/updater:$version"
docker build --network=none -f "$backend/deploy/Dockerfile.api" -t "$api_image" "$backend/deploy"
docker build --network=none --build-arg "NGINX_BASE=$nginx_base" -f "$backend/deploy/Dockerfile.updater" -t "$updater_image" "$backend/deploy"
key="$backend/.release-keys/signing.key"
public="$backend/.release-keys/release.pub"
if [[ ! -f "$key" ]]; then "$staging/voex-release" keygen "$key" "$public"; fi
release=$(mktemp -d "$out/release-$version-XXXXXX")
docker save -o "$release/images.tar" "$api_image"
tar --sort=name -czf "$release/frontend.tar.gz" -C "$staging/web" .
python3 - "$release" "$version" "$previous" "$api_image" "$backend" <<'PY'
import hashlib,json,pathlib,re,subprocess,sys,datetime
p,version,previous,api,backend=sys.argv[1:]
p=pathlib.Path(p)
def digest(file):
 h=hashlib.sha256()
 with file.open('rb') as f:
  for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
 return h.hexdigest()
source='\n'.join(f.read_text() for f in (pathlib.Path(backend)/'internal/database').glob('*.go'))
schema=re.search(r'LatestVersion\s*=\s*"([^"]+)"',source).group(1)
inspect=lambda image:subprocess.check_output(['docker','image','inspect',image,'--format','{{.Id}}'],text=True).strip()
manifest=dict(formatVersion=2,version=version,fromVersions=[previous] if previous else [],schemaVersion=schema,createdAt=datetime.datetime.now(datetime.timezone.utc).isoformat(),notes='固定 Nginx 与版本化前端发布',apiImage=api,apiImageID=inspect(api),imagesSHA256=digest(p/'images.tar'),frontendSHA256=digest(p/'frontend.tar.gz'))
(p/'release.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
PY
"$staging/voex-release" sign "$key" "$release/release.json" "$release/release.sig"
tar -czf "$out/voex-$version-upgrade.tar.gz" -C "$release" release.json release.sig images.tar frontend.tar.gz
install=$(mktemp -d "$out/install-$version-XXXXXX")
cp "$release/"{release.json,release.sig,images.tar,frontend.tar.gz} "$install/"
cp "$backend/deploy/"{compose.yaml,nginx.conf.template,install.sh,migrate-frontend.sh} "$install/"
cp "$staging/voex-release" "$install/"
cp "$public" "$install/release.pub"
docker save -o "$install/infrastructure.tar" "$mysql_image" "$nginx_image" "$updater_image"
python3 - "$install" "$version" "$mysql_image" "$nginx_image" "$updater_image" <<'PY'
import hashlib,json,pathlib,sys,subprocess
p,version,mysql,nginx,updater=sys.argv[1:];p=pathlib.Path(p)
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for c in iter(lambda:f.read(1024*1024),b''):h.update(c)
 return h.hexdigest()
(p/'bootstrap.json').write_text(json.dumps({'version':version,'mysqlImage':mysql,'nginxImage':nginx,'nginxImageID':subprocess.check_output(['docker','image','inspect',nginx,'--format','{{.Id}}'],text=True).strip(),'updaterImage':updater,'mysqlImageID':subprocess.check_output(['docker','image','inspect',mysql,'--format','{{.Id}}'],text=True).strip(),'updaterImageID':subprocess.check_output(['docker','image','inspect',updater,'--format','{{.Id}}'],text=True).strip(),'files':{f:digest(p/f) for f in ['infrastructure.tar','compose.yaml','nginx.conf.template','install.sh','migrate-frontend.sh']}},indent=2)+'\n')
PY
"$staging/voex-release" sign "$key" "$install/bootstrap.json" "$install/bootstrap.sig"
chmod 755 "$install/install.sh" "$install/migrate-frontend.sh" "$install/voex-release"
tar -czf "$out/voex-$version-install.tar.gz" -C "$install" .
printf 'Upgrade package: %s\nInstall package: %s\n' "$out/voex-$version-upgrade.tar.gz" "$out/voex-$version-install.tar.gz"

python3 - "$out" "$release" "$install" <<'PY'
import pathlib,shutil,sys
root=pathlib.Path(sys.argv[1]).resolve()
for value in sys.argv[2:]:
 path=pathlib.Path(value).resolve()
 if path.parent!=root or not path.name.startswith(('release-','install-')):raise SystemExit('Unsafe temporary path')
 shutil.rmtree(path)
PY
