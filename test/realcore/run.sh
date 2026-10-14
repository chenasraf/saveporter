#!/bin/sh
# Runs the accuracy matrix against real emulators. Needs Docker.
#
#   test/realcore/run.sh <states-dir> <roms-dir> [extra-core.so]
#
# The extra core is any mGBA libretro core to test conversions against, such as a Steam Deck's
# RetroArch core (~/.var/app/org.libretro.RetroArch/config/retroarch/cores/mgba_libretro.so).
set -e
[ $# -ge 2 ] || { sed -n '4,8p' "$0"; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
states=$(cd "$1" && pwd)
roms=$(cd "$2" && pwd)
core=
[ -n "$3" ] && core="-v $(cd "$(dirname "$3")" && pwd)/$(basename "$3"):/corpus/core.so:ro"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

docker build -q --platform linux/amd64 -t saveporter-realcore "$here" >/dev/null
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/saveporter" ./cmd/saveporter)
# shellcheck disable=SC2086 # $core is empty or a single -v option
docker run --rm --platform linux/amd64 \
	-v "$here/run.py:/rig/run.py:ro" -v "$work/saveporter:/rig/saveporter:ro" \
	-v "$states:/corpus/states:ro" -v "$roms:/corpus/roms:ro" $core \
	-v "$work:/work" \
	-e LONG -e CROSS \
	saveporter-realcore python3 -u /rig/run.py
