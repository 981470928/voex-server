#!/usr/bin/env bash
# Build the current project and export one complete installer image; never push or install.
set -euo pipefail
umask 077
version=${1:?usage: build-install-image.sh VERSION [DOCKER_REPOSITORY]}
repo=${2:-chenrui9814/voex}
[[ $# -le 2 ]] || { echo 'Usage: build-install-image.sh VERSION [DOCKER_REPOSITORY]' >&2; exit 1; }
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9][A-Za-z0-9.-]*)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
[[ "$repo" =~ ^[a-z0-9][a-z0-9._/-]+$ ]] || { echo 'Invalid Docker repository' >&2; exit 1; }
backend=$(cd "$(dirname "$0")/.." && pwd)
# Do not embed the build host's personal Pexels key in any generated frontend.
VITE_PEXELS_API_KEY= "$backend/deploy/build-release.sh" "$version"
"$backend/deploy/prepare-hub-release.sh" "$version" "$repo"
image="$repo:install-$version"
out="$backend/dist/voex-install-$version.tar"
# Publish the archive path only after docker save has completed successfully.
temporary=$(mktemp "$backend/dist/.voex-install-$version-XXXXXX.tar")
trap 'rm -f -- "$temporary"' EXIT
docker save --output "$temporary" "$image"
mv -- "$temporary" "$out"
printf 'Complete installer image: %s\nExported image archive: %s\nNo installation or registry push performed.\n' "$image" "$out"
