#!/bin/sh
# Runs the Game Boy checks against real emulators. Needs Docker.
#
#   test/realcore/run-saves.sh <cores-dir> [<states-dir> <roms-dir>]
#
# The cores are RetroArch's: gambatte_libretro.so, sameboy_libretro.so, vbam_libretro.so,
# gearboy_libretro.so and mgba_libretro.so, such as a Steam Deck's
# (~/.var/app/org.libretro.RetroArch/config/retroarch/cores). Gambatte as EmulatorJS builds it and
# the mGBA app come from the image. With a folder of Gambatte states and one of their ROMs (each
# state named after its ROM, as RetroArch names them), the state checks run too. ONLY=<name> limits
# the save checks to the test cartridges whose names contain it.
set -e
[ $# -ge 1 ] || { sed -n '4,11p' "$0"; exit 2; }
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
cores=$(cd "$1" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
corpus=
if [ $# -ge 3 ]; then
	corpus="-v $(cd "$2" && pwd):/corpus/gbstates:ro -v $(cd "$3" && pwd):/corpus/gbroms:ro"
fi

docker build -q --platform linux/amd64 -t saveporter-realcore "$here" >/dev/null
(cd "$root" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$work/saveporter" ./cmd/saveporter)
# shellcheck disable=SC2086 # $corpus is empty or a list of -v options
docker run --rm --platform linux/amd64 \
	-v "$here/saves.py:/rig/saves.py:ro" -v "$here/gbrom.py:/rig/gbrom.py:ro" \
	-v "$here/gbstates.py:/rig/gbstates.py:ro" \
	-v "$work/saveporter:/rig/saveporter:ro" -v "$cores:/cores:ro" -v "$work:/work" $corpus \
	-e ONLY saveporter-realcore sh -c '
		status=0
		python3 -u /rig/saves.py || status=1
		if [ -d /corpus/gbstates ]; then python3 -u /rig/gbstates.py || status=1; fi
		exit $status'
