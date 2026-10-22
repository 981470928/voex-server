#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --help || ${1:-} == -h ]]; then
 cat <<'HELP'
Voex complete installer (Linux amd64)
Usage: IMAGE [DEPLOY_DIRECTORY=/opt/voex] [PORT=8080] [IPV4_BIND_ADDRESS=0.0.0.0]
Required Docker options:
  --name voex-installer
  --network host
  -v /var/run/docker.sock:/var/run/docker.sock
  -v /opt/voex:/opt/voex
  -v /home/static:/home/static
For a custom deployment directory, bind the same absolute host path into this container.
Only fresh installations are accepted. Existing deployment files are never overwritten.
HELP
 exit 0
fi
[[ $# -le 3 ]] || { echo 'Too many arguments; use --help.' >&2; exit 1; }
[[ $EUID -eq 0 ]] || { echo 'Run the installer container as root.' >&2; exit 1; }
[[ -S /var/run/docker.sock ]] || { echo 'Bind the host Docker socket: -v /var/run/docker.sock:/var/run/docker.sock' >&2; exit 1; }
# Check mounts and networking before the installer can import images or write data.
python3 - "${1:-/opt/voex}" <<'PY'
import json,os,subprocess,sys
from pathlib import Path
# The kernel reports /run/docker.sock when /var/run is a symlink to /run.
def canonical(path):
 return str(Path(path).resolve())
mounts={canonical(line.split()[4]) for line in Path('/proc/self/mountinfo').read_text().splitlines()}
for path in (sys.argv[1],'/home/static','/var/run/docker.sock'):
 if canonical(path) not in mounts:raise SystemExit('Required bind mount is missing: '+path)
try:
 container=json.loads(subprocess.check_output(['docker','container','inspect',os.environ.get('VOEX_INSTALLER_CONTAINER_NAME','voex-installer')],text=True,stderr=subprocess.DEVNULL))[0]
except (subprocess.CalledProcessError,ValueError,IndexError):
 raise SystemExit('Cannot identify the installer container; use --name voex-installer or set VOEX_INSTALLER_CONTAINER_NAME to its actual name.')
if container['HostConfig']['NetworkMode']!='host':raise SystemExit('Use --network host for installation readiness checks.')
binds={canonical(mount['Destination']):mount for mount in container.get('Mounts',[]) if mount.get('Type')=='bind'}
for path in (sys.argv[1],'/home/static','/var/run/docker.sock'):
 mount=binds.get(canonical(path))
 if not mount or not mount.get('RW'):
  raise SystemExit('Bind this host path read-write at the identical container path: '+path)
 # Only the socket accepts /var/run and /run aliases; data paths stay identical.
 source_matches=(canonical(mount['Source'])==canonical(path)) if path=='/var/run/docker.sock' else (mount['Source']==path and mount['Destination']==path)
 if not source_matches:
  raise SystemExit('Bind this host path read-write at the identical container path: '+path)
if json.loads(subprocess.check_output(['docker','info','--format','{{json .}}'],text=True))['OSType']!='linux':
 raise SystemExit('The host Docker daemon must run Linux containers.')
print('Installer bind mounts and host networking verified.',flush=True)
PY
exec /release/install-hub.sh "$@"
