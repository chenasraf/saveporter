#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>
#include <time.h>
#include <sys/time.h>
// Freezes the wall clock the cart RTC reads, so runs started at different moments compare equal.
// TIMESHIM_NOW sets the frozen time (unix seconds), to play a save some time after it was written.
// TIMESHIM_TICK=1 makes the first read a second early: Gearboy catches its clock up only once the
// wall clock differs from what it read at reset, which a frozen clock never does.
static time_t now(void) {
	static int reads;
	const char *s = getenv("TIMESHIM_NOW");
	time_t t = s ? (time_t) strtoll(s, NULL, 10) : 1760400000;
	if (getenv("TIMESHIM_TICK") && reads++ == 0) return t - 1;
	return t;
}
time_t time(time_t *t) { time_t n = now(); if (t) *t = n; return n; }
int gettimeofday(struct timeval *tv, void *tz) {
	if (tv) { tv->tv_sec = now(); tv->tv_usec = 0; }
	return 0;
}
// Only the wall clock is frozen: cores time frames and audio with the monotonic clocks.
int clock_gettime(clockid_t id, struct timespec *ts) {
	static int (*real)(clockid_t, struct timespec *);
	if (id != CLOCK_REALTIME) {
		if (!real) real = (int (*)(clockid_t, struct timespec *)) dlsym(RTLD_NEXT, "clock_gettime");
		return real(id, ts);
	}
	ts->tv_sec = now(); ts->tv_nsec = 0;
	return 0;
}
