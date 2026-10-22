#!/usr/bin/env bash
set -euo pipefail
umask 077
here=$(cd "$(dirname "$0")" && pwd)
deploy=${1:-/home/voex-deploy}
port=${2:-18080}
bind=${3:-0.0.0.0}
[[ "$deploy" =~ ^/[A-Za-z0-9_/-]+$ && "$deploy" != / ]] || { echo 'Invalid absolute deployment directory' >&2; exit 1; }
[[ "$port" =~ ^[0-9]+$ && "$port" -gt 1024 && "$port" -le 65535 ]] || exit 1
[[ "$bind" =~ ^[0-9.]+$ ]] || exit 1
[[ ! -e "$deploy/runtime.env" ]] || { echo 'Deployment already exists; use its upgrade interface.' >&2; exit 1; }
command -v docker >/dev/null
command -v python3 >/dev/null
docker compose version >/dev/null
"$here/voex-release" verify "$here/release.pub" "$here/release.json" "$here/release.sig"
"$here/voex-release" verify "$here/release.pub" "$here/bootstrap.json" "$here/bootstrap.sig"
python3 - "$here" <<'PY'
import hashlib,json,pathlib,sys
p=pathlib.Path(sys.argv[1]);manifest=json.loads((p/'release.json').read_text());bootstrap=json.loads((p/'bootstrap.json').read_text())
assert manifest.get('formatVersion')==2, 'Unsupported release format'
expected={**bootstrap['files'],'images.tar':manifest['imagesSHA256'],'frontend.tar.gz':manifest['frontendSHA256']}
for name,wanted in expected.items():
 h=hashlib.sha256()
 with (p/name).open('rb') as f:
  for c in iter(lambda:f.read(1024*1024),b''):h.update(c)
 if h.hexdigest()!=wanted:raise SystemExit('Checksum mismatch: '+name)
PY
docker load -i "$here/infrastructure.tar"
docker load -i "$here/images.tar"
mkdir -p "$deploy/"{state,backups,releases,secrets} /home/static
cp "$here/compose.yaml" "$here/nginx.conf.template" "$here/release.pub" "$deploy/"
python3 - "$here" "$deploy" "$port" "$bind" <<'PY'
import json,pathlib,secrets,sys,subprocess
source,deploy,port,bind=sys.argv[1:];source=pathlib.Path(source);p=pathlib.Path(deploy)
r=json.loads((source/'release.json').read_text());b=json.loads((source/'bootstrap.json').read_text())
for image,wanted in [(r['apiImage'],r['apiImageID']),(b['nginxImage'],b['nginxImageID']),(b['mysqlImage'],b['mysqlImageID']),(b['updaterImage'],b['updaterImageID'])]:
 actual=subprocess.check_output(['docker','image','inspect',image,'--format','{{.Id}}'],text=True).strip()
 if actual!=wanted:raise SystemExit('Image identity mismatch')
env={'COMPOSE_PROJECT_NAME':'voex-local','VOEX_DEPLOY_DIR':deploy,'VOEX_STATIC_ROOT':'/home/static','VOEX_HTTP_PORT':port,'VOEX_BIND_ADDRESS':bind,'VOEX_VERSION':r['version'],'VOEX_SCHEMA_VERSION':r['schemaVersion'],'VOEX_API_IMAGE':r['apiImageID'],'VOEX_NGINX_IMAGE':b['nginxImageID'],'VOEX_UPDATER_IMAGE':b['updaterImageID'],'VOEX_MYSQL_IMAGE':b['mysqlImageID'],'MYSQL_DATABASE':'voex','MYSQL_USER':'voex','MYSQL_PASSWORD':secrets.token_hex(24),'MYSQL_ROOT_PASSWORD':secrets.token_hex(24),'VOEX_UPDATER_TOKEN':secrets.token_hex(32) }
(p/'runtime.env').write_text(''.join(k+'='+v+'\n' for k,v in env.items()));(p/'runtime.env').chmod(0o600)
(p/'secrets/jwt-key').write_bytes(secrets.token_bytes(64));(p/'secrets/jwt-key').chmod(0o600)
(p/'state/maintenance').write_text('initial installation\n')
subprocess.run([str(source/'voex-release'),'frontend-install',str(source/'frontend.tar.gz'),str(p/'frontend'),r['version']],check=True)
subprocess.run([str(source/'voex-release'),'frontend-switch',str(p/'frontend'),r['version']],check=True)
PY
compose=(docker compose --env-file "$deploy/runtime.env" -f "$deploy/compose.yaml")
"${compose[@]}" config --quiet
"${compose[@]}" up -d --wait db updater
"${compose[@]}" run --rm --no-deps api migrate
"${compose[@]}" up -d --no-deps api web
ready=false
for attempt in $(seq 1 60); do
 if curl --fail --silent "http://127.0.0.1:$port/api/health/ready" >/dev/null && curl --fail --silent "http://127.0.0.1:$port/app/version.json" >/dev/null; then ready=true; break; fi
 sleep 2
done
if [[ "$ready" != true ]]; then echo 'Readiness failed; maintenance retained. Inspect compose logs.' >&2; exit 1; fi
rm -- "$deploy/state/maintenance"
printf 'Voex installed at http://%s:%s/app/\nRuntime config: %s/runtime.env\n' "$bind" "$port" "$deploy"
