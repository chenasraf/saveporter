// Libretro harness: harness <core.so> <rom> <state-in|-> <frames> <state-out|->
// Loads the ROM, optionally unserializes a raw core state, runs frames, prints hashes of WRAM and
// the last frame, and optionally writes the core's serialized state. HARNESS_INPUT=1 presses a
// fixed button pattern so the game moves; HARNESS_PPM=<path> dumps the last frame.
#include <dlfcn.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "libretro.h"

static uint64_t frame_hash;
static uint64_t fnv(const void *p, size_t n) {
	const uint8_t *b = p;
	uint64_t h = 1469598103934665603ULL;
	for (size_t i = 0; i < n; i++) { h ^= b[i]; h *= 1099511628211ULL; }
	return h;
}

static void logcb(enum retro_log_level level, const char *fmt, ...) {
	if (level < RETRO_LOG_WARN) return;
	va_list ap; va_start(ap, fmt);
	fprintf(stderr, "  [core] "); vfprintf(stderr, fmt, ap);
	va_end(ap);
}

static bool env(unsigned cmd, void *data) {
	switch (cmd) {
	case RETRO_ENVIRONMENT_GET_LOG_INTERFACE:
		((struct retro_log_callback *) data)->log = logcb; return true;
	case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT: return true;
	case RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY:
	case RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY:
		*(const char **) data = getenv("HARNESS_DIR"); return true;
	default: return false;
	}
}
static uint16_t last_frame[240 * 160];
static void video(const void *d, unsigned w, unsigned h, size_t pitch) {
	if (!d) return;
	frame_hash = fnv(d, h * pitch);
	for (unsigned y = 0; y < h && y < 160; y++) memcpy(&last_frame[y * 240], (const uint8_t *) d + y * pitch, 480);
}
static void dump_ppm(const char *path) {
	FILE *f = fopen(path, "wb");
	fprintf(f, "P6\n240 160\n255\n");
	for (int i = 0; i < 240 * 160; i++) {
		uint16_t p = last_frame[i];
		uint8_t rgb[3] = { (p >> 11) << 3, ((p >> 5) & 63) << 2, (p & 31) << 3 };
		fwrite(rgb, 1, 3, f);
	}
	fclose(f);
}
static void audio(int16_t l, int16_t r) {}
static size_t audio_batch(const int16_t *d, size_t f) { return f; }
static void poll(void) {}
static int frame_no;
static int16_t input(unsigned port, unsigned dev, unsigned idx, unsigned id) {
	if (!getenv("HARNESS_INPUT") || port) return 0;
	int f = frame_no % 240;
	if (id == RETRO_DEVICE_ID_JOYPAD_A) return (frame_no % 30) < 3;
	if (id == RETRO_DEVICE_ID_JOYPAD_RIGHT) return f < 60;
	if (id == RETRO_DEVICE_ID_JOYPAD_DOWN) return f >= 60 && f < 120;
	if (id == RETRO_DEVICE_ID_JOYPAD_LEFT) return f >= 120 && f < 180;
	if (id == RETRO_DEVICE_ID_JOYPAD_UP) return f >= 180;
	return 0;
}

static void *readall(const char *path, size_t *n) {
	FILE *f = fopen(path, "rb");
	if (!f) { perror(path); exit(2); }
	fseek(f, 0, SEEK_END); *n = ftell(f); fseek(f, 0, SEEK_SET);
	void *b = malloc(*n); fread(b, 1, *n, f); fclose(f);
	return b;
}

#define SYM(name) typeof(&name) p_##name = dlsym(core, #name)

int main(int argc, char **argv) {
	if (argc != 6) { fprintf(stderr, "usage\n"); return 2; }
	void *core = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
	if (!core) { fprintf(stderr, "%s\n", dlerror()); return 2; }
	SYM(retro_set_environment); SYM(retro_set_video_refresh); SYM(retro_set_audio_sample);
	SYM(retro_set_audio_sample_batch); SYM(retro_set_input_poll); SYM(retro_set_input_state);
	SYM(retro_init); SYM(retro_load_game); SYM(retro_run); SYM(retro_serialize_size);
	SYM(retro_serialize); SYM(retro_unserialize); SYM(retro_get_memory_data);
	SYM(retro_get_memory_size);
	p_retro_set_environment(env); p_retro_set_video_refresh(video);
	p_retro_set_audio_sample(audio); p_retro_set_audio_sample_batch(audio_batch);
	p_retro_set_input_poll(poll); p_retro_set_input_state(input);
	p_retro_init();
	size_t romn; void *rom = readall(argv[2], &romn);
	struct retro_game_info gi = { argv[2], rom, romn, NULL };
	if (!p_retro_load_game(&gi)) { fprintf(stderr, "load_game failed\n"); return 2; }
	p_retro_run();
	if (strcmp(argv[3], "-")) {
		size_t sn; void *st = readall(argv[3], &sn);
		bool ok = p_retro_unserialize(st, sn);
		printf("unserialize: %s\n", ok ? "ok" : "FAILED");
		if (!ok) return 1;
	}
	int frames = atoi(argv[4]);
	for (int i = 0; i < frames; i++) { frame_no = i; p_retro_run(); }
	void *wram = p_retro_get_memory_data(RETRO_MEMORY_SYSTEM_RAM);
	size_t wn = p_retro_get_memory_size(RETRO_MEMORY_SYSTEM_RAM);
	printf("wram %016llx (%zu bytes)  frame %016llx\n", (unsigned long long) fnv(wram, wn), wn,
		(unsigned long long) frame_hash);
	if (getenv("HARNESS_PPM")) dump_ppm(getenv("HARNESS_PPM"));
	if (strcmp(argv[5], "-")) {
		size_t n = p_retro_serialize_size();
		void *out = malloc(n);
		if (!p_retro_serialize(out, n)) { fprintf(stderr, "serialize failed\n"); return 1; }
		FILE *f = fopen(argv[5], "wb"); fwrite(out, 1, n, f); fclose(f);
		printf("serialized %zu bytes, magic %08x\n", n, *(uint32_t *) out);
	}
	return 0;
}
