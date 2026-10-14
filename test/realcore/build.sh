#!/bin/sh
# Builds the emulators and harnesses into /rig/bin. Runs in the image (see Dockerfile).
set -e
B=/rig/bin
mkdir -p $B /build
OPTS="-G Ninja -DCMAKE_BUILD_TYPE=Release -DBUILD_QT=OFF -DBUILD_SDL=OFF -DUSE_FFMPEG=OFF -DUSE_LUA=OFF \
	-DUSE_SQLITE3=OFF -DUSE_ELF=OFF -DUSE_EDITLINE=OFF -DUSE_DISCORD_RPC=OFF -DUSE_EPOXY=OFF \
	-DBUILD_GL=OFF -DBUILD_GLES2=OFF -DBUILD_GLES3=OFF -DUSE_LIBZIP=OFF -DUSE_MINIZIP=OFF -DUSE_LZMA=OFF \
	-DUSE_JSON_C=OFF -DENABLE_SCRIPTING=OFF -DUSE_FREETYPE=OFF"

# libretro cores. Older mGBA leaves the fd-backed file code out of its libretro target, which still
# links on Linux and only fails on load, so add it where missing and refuse undefined symbols.
for v in v7 vB; do
	src=/src/mgba-$v
	if ! grep -q 'libretro SHARED.*vfs-fd' $src/CMakeLists.txt && ! grep -q 'CORE_VFS_SRC.*vfs-fd' $src/CMakeLists.txt; then
		sed -i 's|_libretro SHARED ${CORE_SRC} ${RETRO_SRC}|& ${CMAKE_CURRENT_SOURCE_DIR}/src/util/vfs/vfs-fd.c|' $src/CMakeLists.txt
	fi
	cmake -S $src -B /build/core-$v $OPTS -DBUILD_LIBRETRO=ON -DBUILD_SHARED=OFF -DBUILD_STATIC=OFF \
		-DCMAKE_C_FLAGS=-DENABLE_VFS_FD >/dev/null
	ninja -C /build/core-$v mgba_libretro >/dev/null
	cp /build/core-$v/mgba_libretro.so $B/core-$v.so
	if ldd -r $B/core-$v.so 2>&1 | grep 'undefined symbol'; then
		echo "core-$v.so has undefined symbols" >&2
		exit 1
	fi
done

# The mGBA app's state code, as a static library with libpng for its PNG states
cmake -S /src/mgba-app -B /build/app $OPTS -DBUILD_LIBRETRO=OFF -DBUILD_SHARED=OFF -DBUILD_STATIC=ON \
	-DUSE_PNG=ON -DUSE_ZLIB=ON -DBUILD_PERF=OFF -DBUILD_TEST=OFF -DBUILD_SUITE=OFF -DBUILD_CINEMA=OFF \
	-DBUILD_UPDATER=OFF >/dev/null
ninja -C /build/app mgba >/dev/null
DEFS=$(ninja -C /build/app -t commands mgba | grep "src/core/core.c" | head -1 | tr ' ' '\n' | grep '^-D' | tr '\n' ' ')
# shellcheck disable=SC2086 # DEFS is a list of flags
gcc -O2 $DEFS -I /src/mgba-app/include -I /build/app/include -o $B/sa /rig/sa.c /build/app/libmgba.a \
	-lpng -lz -lm -lpthread

gcc -O2 -I /src/mgba-vB/src/platform/libretro -o $B/harness /rig/harness.c -ldl
gcc -O2 -shared -fPIC -o $B/timeshim.so /rig/timeshim.c
echo "built: $(ls $B)"
