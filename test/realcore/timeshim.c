#include <time.h>
#include <sys/time.h>
// Freezes the wall clock the cart RTC reads, so runs started at different moments compare equal.
time_t time(time_t *t) { if (t) *t = 1760400000; return 1760400000; }
