#!/usr/bin/env bash
set -euo pipefail
umask 077

here=$(cd "$(dirname "$0")" && pwd)
deploy=${1:-/opt/voex}
port=${2:-8080}
bind=${3:-0.0.0.0}
project=${VOEX_PROJECT_NAME:-voex}
fail() { printf '%s\n' "$*" >&2; exit 1; }
[[ $# -le 3 ]] || fail 'Usage: install-hub.sh [DEPLOY_DIRECTORY] [PORT] [IPV4_BIND_ADDRESS]'
[[ $EUID -eq 0 ]] || fail 'Run this installer as root, for example with sudo.'
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || fail 'This release supports Linux amd64 only.'
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail 'VOEX_PROJECT_NAME must contain 1-63 lowercase letters, digits, underscores or hyphens and start with a letter or digit.'
[[ "$port" =~ ^[0-9]{1,5}$ ]] || fail 'PORT must be an integer between 1 and 65535.'
port=$((10#$port))
[[ "$port" -ge 1 && "$port" -le 65535 ]] || fail 'PORT must be between 1 and 65535.'
[[ "$deploy" =~ ^/[A-Za-z0-9_./-]+$ && "$deploy" != / ]] || fail 'Use a dedicated absolute deployment path without spaces.'
[[ ! -e "$deploy/runtime.env" && ! -L "$deploy/runtime.env" ]] || fail 'Deployment already exists; use its upgrade interface instead of overwriting runtime.env.'
for tool in docker python3 install; do command -v "$tool" >/dev/null || fail "Required command is missing: $tool"; done
docker compose version >/dev/null
[[ -x "$here/voex-release" ]] || fail 'The bundled voex-release executable is missing or not executable.'
"$here/voex-release" verify "$here/release.pub" "$here/hub.json" "$here/hub.sig"

# Validate all signed input before changing deployment files or starting services.
# Passwords, JWT keys and the updater token are generated only inside Python and
# are never interpolated into shell arguments or written to standard output.
python3 - "$here" "$deploy" "$port" "$bind" "$project" <<'PY'
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
from urllib.parse import urlsplit

source, target, port, bind, project = sys.argv[1:]
source, target = Path(source), Path(target)

def fail(message):
    raise SystemExit(message)

def unique_object(pairs):
    result = {}
    for name, value in pairs:
        if name in result:
            fail('Duplicate manifest key: ' + name)
        result[name] = value
    return result

def hash_file(path):
    value = hashlib.sha256()
    with path.open('rb') as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()

def real_path(path):
    for component in (path, *path.parents):
        if component.is_symlink():
            fail('Directory paths must not contain symlinks: ' + str(component))
        if component.exists() and not component.is_dir():
            fail('A directory path is occupied by a file: ' + str(component))

if str(target) != sys.argv[2] or any(part in ('.', '..') for part in sys.argv[2].split('/')):
    fail('Use a normalized absolute deployment path without . or .. components.')
static = Path('/home/static')
if target == static or target in static.parents or static in target.parents:
    fail('The deployment directory must be separate from /home/static.')
real_path(target)
real_path(static)
if (target / 'runtime.env').exists() or (target / 'runtime.env').is_symlink():
    fail('Deployment already exists; runtime.env will not be overwritten.')
if target.exists() and any(target.iterdir()):
    fail('The deployment directory must be absent or empty for a fresh installation.')
try:
    bind = str(ipaddress.IPv4Address(bind))
except ipaddress.AddressValueError:
    fail('BIND_ADDRESS must be an IPv4 address, such as 0.0.0.0 or 127.0.0.1.')

try:
    manifest = json.loads((source / 'hub.json').read_text(), object_pairs_hook=unique_object)
except (OSError, ValueError):
    fail('Cannot read the signed Hub manifest.')
if not isinstance(manifest, dict) or manifest.get('format') != 'voex-hub-install-v2':
    fail('Unsupported Hub installation format.')
version = manifest.get('version')
if not isinstance(version, str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9][A-Za-z0-9.-]{0,63})?', version):
    fail('Invalid release version.')
schema = manifest.get('schemaVersion')
if not isinstance(schema, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}', schema):
    fail('Invalid schema version.')
expected_names = {'compose.yaml', 'nginx.conf.template', 'install-hub.sh'}
files = manifest.get('files')
if not isinstance(files, dict) or set(files) != expected_names:
    fail('The signed manifest must checksum compose.yaml, nginx.conf.template and install-hub.sh.')
checks = dict(files)
checks['frontend.tar.gz'] = manifest.get('frontendSHA256')
checks['images.tar'] = manifest.get('imagesSHA256')
for name, wanted in checks.items():
    path = source / name
    if not isinstance(wanted, str) or not re.fullmatch(r'[a-f0-9]{64}', wanted):
        fail('Invalid signed checksum: ' + name)
    if path.is_symlink() or not path.is_file() or hash_file(path) != wanted:
        fail('Signed file checksum mismatch: ' + name)
for name in ('voex-release', 'release.pub', 'hub.json', 'hub.sig', 'LICENSE'):
    if (source / name).is_symlink() or not (source / name).is_file():
        fail('Required release file is missing or not a regular file: ' + name)

images = []
for role in ('api', 'updater', 'nginx', 'mysql'):
    image, wanted = manifest.get(role + 'Image'), manifest.get(role + 'ImageID')
    if not isinstance(image, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:/-]*(?:@sha256:[a-f0-9]{64})?', image):
        fail('Invalid signed image reference: ' + role)
    if not isinstance(wanted, str) or not re.fullmatch(r'sha256:[a-f0-9]{64}', wanted):
        fail('Invalid signed image ID: ' + role)
    images.append((role, image, wanted))
if manifest['apiImage'] != 'voex/api:' + version or manifest['updaterImage'] != 'voex/updater:' + version:
    fail('Bundled API and updater tags must match the signed release version.')

# A new deployment directory must not adopt another deployment's database volume.
for resource in ('container', 'volume'):
    present = subprocess.check_output(['docker', resource, 'ls', '--all', '--filter', 'label=com.docker.compose.project=' + project, '--format', '{{.ID}}'] if resource == 'container' else ['docker', 'volume', 'ls', '--filter', 'label=com.docker.compose.project=' + project, '--format', '{{.Name}}'], text=True).strip()
    if present:
        fail('Compose project already has containers or volumes; choose a new VOEX_PROJECT_NAME.')

origins = os.environ.get('VOEX_ALLOWED_ORIGINS')
if origins is None:
    origins = 'http://' + ('127.0.0.1' if bind == '0.0.0.0' else bind) + ':' + port
if not origins or any(character in origins for character in '\r\n\x00$#'):
    fail('VOEX_ALLOWED_ORIGINS must be a nonempty comma-separated list of HTTP(S) origins.')
normalized_origins = []
for value in origins.split(','):
    value = value.strip()
    try:
        parsed = urlsplit(value)
        valid = parsed.scheme in ('http', 'https') and parsed.hostname and parsed.port != 0 and not parsed.username and not parsed.password and not parsed.query and not parsed.fragment and not parsed.path
    except ValueError:
        valid = False
    if not valid:
        fail('VOEX_ALLOWED_ORIGINS entries must be origins only, for example https://docs.example.com without a trailing slash.')
    normalized_origins.append(value)

# The complete signed archive contains all four runtime images; no registry pull.
subprocess.run(['docker', 'load', '--input', str(source / 'images.tar')], check=True)
for role, image, wanted in images:
    try:
        actual = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image], text=True))[0]
    except (subprocess.CalledProcessError, ValueError, IndexError):
        fail('Cannot inspect the required image: ' + role)
    if actual.get('Id') != wanted or actual.get('Os') != 'linux' or actual.get('Architecture') != 'amd64':
        fail('Signed image ID or Linux amd64 platform mismatch: ' + role)

# All images are fixed by content ID in runtime.env, even if a tag later moves.
target.mkdir(mode=0o700, parents=True, exist_ok=True)
for directory in ('state', 'backups', 'releases', 'secrets'):
    (target / directory).mkdir(mode=0o700)
os.chmod(target / 'state', 0o755)
static.mkdir(mode=0o700, parents=True, exist_ok=True)
for name in ('compose.yaml', 'nginx.conf.template', 'release.pub', 'LICENSE'):
    shutil.copyfile(source / name, target / name)
    os.chmod(target / name, 0o644 if name in ('nginx.conf.template', 'release.pub', 'LICENSE') else 0o600)
values = {
    'COMPOSE_PROJECT_NAME': project,
    'VOEX_DEPLOY_DIR': str(target),
    'VOEX_STATIC_ROOT': str(static),
    'VOEX_HTTP_PORT': port,
    'VOEX_BIND_ADDRESS': bind,
    'VOEX_VERSION': version,
    'VOEX_SCHEMA_VERSION': schema,
    'VOEX_API_IMAGE': manifest['apiImageID'],
    'VOEX_NGINX_IMAGE': manifest['nginxImageID'],
    'VOEX_UPDATER_IMAGE': manifest['updaterImageID'],
    'VOEX_MYSQL_IMAGE': manifest['mysqlImageID'],
    'MYSQL_DATABASE': 'voex',
    'MYSQL_USER': 'voex',
    'MYSQL_PASSWORD': secrets.token_hex(24),
    'MYSQL_ROOT_PASSWORD': secrets.token_hex(24),
    'VOEX_UPDATER_TOKEN': secrets.token_hex(32)
}
with open(target / 'runtime.env', 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as file:
    file.write(''.join(name + '=' + value + '\n' for name, value in values.items()))
with open(target / 'secrets/jwt-key', 'xb', opener=lambda path, flags: os.open(path, flags, 0o600)) as file:
    file.write(secrets.token_bytes(64))
(target / 'state/maintenance').write_text('initial Hub installation\n')
os.chmod(target / 'state/maintenance', 0o644)
subprocess.run([str(source / 'voex-release'), 'frontend-install', str(source / 'frontend.tar.gz'), str(target / 'frontend'), version], check=True)
subprocess.run([str(source / 'voex-release'), 'frontend-switch', str(target / 'frontend'), version], check=True)
PY

compose=(docker compose --project-name "$project" --project-directory "$deploy" --env-file "$deploy/runtime.env" -f "$deploy/compose.yaml")
"${compose[@]}" config --quiet
"${compose[@]}" up -d --wait db updater
"${compose[@]}" run --rm --no-deps api migrate
"${compose[@]}" up -d --no-deps api web

# Keep maintenance enabled until both externally served versions and the schema
# match the signed manifest; a plain 200 or a SPA fallback is not sufficient.
python3 - "$here/hub.json" "$bind" "$port" <<'PY'
import json
import sys
import time
import urllib.error
import urllib.request

manifest = json.load(open(sys.argv[1]))
host = '127.0.0.1' if sys.argv[2] == '0.0.0.0' else sys.argv[2]
base = 'http://' + host + ':' + sys.argv[3]
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

def fetch(path):
    request = urllib.request.Request(base + path, headers={'Cache-Control': 'no-cache'})
    with client.open(request, timeout=5) as response:
        if response.status != 200:
            return None
        return json.loads(response.read(65537))

deadline = time.monotonic() + 180
while time.monotonic() < deadline:
    try:
        api = fetch('/api/health/ready')
        frontend = fetch('/app/version.json')
        if api and frontend and api.get('status') == 'ok' and api.get('version') == manifest['version'] and api.get('schemaVersion') == manifest['schemaVersion'] and frontend.get('version') == manifest['version']:
            break
    except (OSError, ValueError, urllib.error.HTTPError):
        pass
    time.sleep(2)
else:
    raise SystemExit('Readiness/version verification failed; maintenance retained. Inspect this Compose project before recovery.')
PY
rm -- "$deploy/state/maintenance"
printf 'Voex %s installed at http://%s:%s/app/\nRuntime config: %s/runtime.env\nCompose project: %s\n' \
    "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$here/hub.json")" \
    "$bind" "$port" "$deploy" "$project"
