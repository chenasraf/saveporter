# The emulators the accuracy matrix runs, built for linux/amd64 (the Steam Deck's platform, so a
# Deck's own RetroArch core can run beside them).
FROM --platform=linux/amd64 debian:bookworm

RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
	ca-certificates git gcc g++ libc6-dev cmake ninja-build pkg-config libpng-dev zlib1g-dev \
	p7zip-full python3 && rm -rf /var/lib/apt/lists/*

# mGBA at the versions that matter: the last commit writing state v7 (EmulatorJS 4.2.3's core
# version), a current one (vB), and the 0.10.5 app.
ARG MGBA_V7=e95b81f1f7b95161fbda81fa5e931e3bcb193ccf
ARG MGBA_VB=143adf5aa2a60c6036810b0cd72445e13d74b0e6
ARG MGBA_APP=0.10.5
RUN git clone -q https://github.com/mgba-emu/mgba.git /src/mgba && \
	for v in v7:$MGBA_V7 vB:$MGBA_VB app:$MGBA_APP; do \
		git -C /src/mgba worktree add -q /src/mgba-${v%%:*} ${v#*:}; \
	done

COPY harness.c sa.c timeshim.c build.sh /rig/
RUN sh /rig/build.sh
