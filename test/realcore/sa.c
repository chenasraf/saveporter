// The mGBA app's state code, on mGBA 0.10's core API:
//   sa save <rom> <frames> <out>        boot, play scripted input, write a PNG state as the app does
//   sa load <rom> <in> <frames> <out|-> load any state, play, print frame hash, write a raw state
//   sa sav <rom> <sav> <frames> <wram-out>
//                                       play with <sav> as the battery save, as the app does: the
//                                       app writes the save back into the same file; then dump
//                                       the first 0x200 bytes of WRAM
#include <mgba/core/core.h>
#include <mgba/core/config.h>
#include <mgba/core/serialize.h>
#include <mgba-util/vfs.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static uint64_t fnv(const void *p, size_t n) {
	const uint8_t *b = p;
	uint64_t h = 1469598103934665603ULL;
	for (size_t i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
	return h;
}

static void play(struct mCore *core, int frames) {
	for (int i = 0; i < frames; i++) {
		int f = i % 240, keys = 0;
		if (i % 30 < 3) keys |= 1; // A
		keys |= f < 60 ? 1 << 4 : f < 120 ? 1 << 7 : f < 180 ? 1 << 5 : 1 << 6;
		core->setKeys(core, keys);
		core->runFrame(core);
	}
}

int main(int argc, char **argv) {
	if (argc < 5) return 2;
	struct mCore *core = mCoreFind(argv[2]);
	if (!core || !core->init(core)) { fprintf(stderr, "no core\n"); return 2; }
	mCoreInitConfig(core, NULL);
	unsigned w, h;
	core->desiredVideoDimensions(core, &w, &h);
	color_t *buf = calloc(w * h, sizeof(color_t));
	core->setVideoBuffer(core, buf, w);
	if (!mCoreLoadFile(core, argv[2])) { fprintf(stderr, "no rom\n"); return 2; }
	if (!strcmp(argv[1], "sav")) {
		struct VFile *sav = VFileOpen(argv[3], O_CREAT | O_RDWR);
		if (!sav || !core->loadSave(core, sav)) { fprintf(stderr, "no save\n"); return 2; }
		core->reset(core);
		play(core, atoi(argv[4]));
		FILE *f = fopen(argv[5], "wb");
		for (uint32_t a = 0xC000; a < 0xC200; a++) fputc(core->rawRead8(core, a, -1), f);
		fclose(f);
		// Unloading the ROM writes the save back, footer included.
		core->unloadROM(core);
		core->deinit(core);
		printf("sav: ok\n");
		return 0;
	}
	core->reset(core);
	core->runFrame(core);
	if (!strcmp(argv[1], "save")) {
		play(core, atoi(argv[3]));
		struct VFile *vf = VFileOpen(argv[4], O_CREAT | O_TRUNC | O_RDWR);
		bool ok = mCoreSaveStateNamed(core, vf, SAVESTATE_SCREENSHOT | SAVESTATE_SAVEDATA | SAVESTATE_RTC | SAVESTATE_METADATA);
		vf->close(vf);
		// The same moment again, raw: what the PNG must decode to.
		char raw[1024];
		snprintf(raw, sizeof(raw), "%s.mgbaraw", argv[4]);
		vf = VFileOpen(raw, O_CREAT | O_TRUNC | O_RDWR);
		ok = ok && mCoreSaveStateNamed(core, vf, SAVESTATE_SAVEDATA | SAVESTATE_RTC | SAVESTATE_METADATA);
		vf->close(vf);
		printf("save: %s\n", ok ? "ok" : "FAILED");
		return !ok;
	}
	struct VFile *vf = VFileOpen(argv[3], O_RDONLY);
	bool ok = mCoreLoadStateNamed(core, vf, SAVESTATE_SCREENSHOT | SAVESTATE_RTC);
	vf->close(vf);
	printf("load: %s\n", ok ? "ok" : "FAILED");
	if (!ok) return 1;
	play(core, atoi(argv[4]));
	printf("frame %016llx\n", (unsigned long long) fnv(buf, w * h * sizeof(color_t)));
	if (argc > 5 && strcmp(argv[5], "-")) {
		vf = VFileOpen(argv[5], O_CREAT | O_TRUNC | O_RDWR);
		mCoreSaveStateNamed(core, vf, SAVESTATE_SAVEDATA | SAVESTATE_RTC);
		vf->close(vf);
	}
	return 0;
}
