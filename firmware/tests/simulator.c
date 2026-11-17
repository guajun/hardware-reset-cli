/* Real protocol core behind a stdin/stdout byte transport; no hardware access. */
#include "reset_protocol.h"
#include <stdint.h>
#include <stdio.h>
#ifdef _WIN32
#include <io.h>
#include <windows.h>
static uint32_t now_ms(void) {
  return (uint32_t)GetTickCount();
}
#else
#include <sys/select.h>
#include <time.h>
#include <unistd.h>
static uint32_t now_ms(void) {
  struct timespec t;
  clock_gettime(CLOCK_MONOTONIC, &t);
  return (uint32_t)((uint64_t)t.tv_sec * 1000 + t.tv_nsec / 1000000);
}
#endif
static void gpio(unsigned pin, int asserted) {
  (void)pin;
  (void)asserted;
}
int main(void) {
  HR_Init("0123456789ABCDEF01234567", gpio);
  for (;;) {
    uint8_t buf[256];
    size_t length = 0;
#ifdef _WIN32
    DWORD available = 0, got = 0;
    HANDLE input = GetStdHandle(STD_INPUT_HANDLE);
    if (!PeekNamedPipe(input, NULL, 0, NULL, &available, NULL))
      break;
    if (available) {
      if (!ReadFile(input, buf, available > sizeof buf ? sizeof buf : available, &got, NULL))
        break;
      length = got;
    } else
      Sleep(1);
#else
    fd_set set;
    FD_ZERO(&set);
    FD_SET(0, &set);
    struct timeval wait = {0, 1000};
    int ready = select(1, &set, NULL, NULL, &wait);
    if (ready < 0)
      break;
    if (ready) {
      ssize_t n = read(0, buf, sizeof buf);
      if (n <= 0)
        break;
      length = (size_t)n;
    }
#endif
    uint32_t now = now_ms();
    if (length)
      HR_Feed(buf, length, now);
    HR_Poll(now);
    const char *s;
    while ((s = HR_TxPeek()) != NULL) {
      fputs(s, stdout);
      fflush(stdout);
      HR_TxPop();
    }
  }
  HR_ResetSession();
  return 0;
}
