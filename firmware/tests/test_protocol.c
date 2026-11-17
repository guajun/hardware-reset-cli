#include "reset_protocol.h"
#undef NDEBUG
#include <assert.h>
#include <stdio.h>
#include <string.h>
static unsigned low, edges;
static void gpio(unsigned pin, int asserted) {
  if (asserted) {
    low |= 1U << pin;
    edges++;
  } else
    low &= ~(1U << pin);
}
static void feed(const char *s, uint32_t now) {
  HR_Feed((const uint8_t *)s, strlen(s), now);
}
static void expect(const char *s) {
  assert(HR_TxPeek());
  assert(!strcmp(HR_TxPeek(), s));
  HR_TxPop();
}
int main(void) {
  HR_Init("0123456789ABCDEF01234567", gpio);
  feed("V1 1 RESET PA0 100\n", 0);
  expect("V1 1 ERR HANDSHAKE_REQUIRED\n");
  assert(!low);
  feed("V1 2 HELLO 1\r\n", 0);
  expect("V1 2 OK HELLO HWRESET 1\n");
  feed("V1 3 RESET PA0 100\n", 10);
  assert(low == 1);
  assert(!HR_TxPeek());
  feed("V1 3 RESET PA0 100\n", 11);
  assert(edges == 1);
  feed("V1 4 RESET PA1 1\n", 12);
  expect("V1 4 ERR BUSY\n");
  HR_Poll(109);
  assert(low == 1);
  HR_Poll(110);
  assert(!low);
  expect("V1 3 OK RESET PA0 RELEASED 100\n");
  feed("V1 3 RESET PA0 100\n", 111);
  expect("V1 3 OK RESET PA0 RELEASED 100\n");
  assert(edges == 1);
  feed("V1 3 RESET PA1 100\n", 112);
  expect("V1 3 ERR ID_CONFLICT\n");
  feed("V1 5 RESET PA0 500\nV1 6 RELEASE PA0\n", 120);
  assert(!low);
  expect("V1 5 ERR CANCELLED\n");
  expect("V1 6 OK RELEASE PA0 RELEASED\n");
  feed("V1 7 ASSERT PA7 10\n", UINT32_MAX - 4);
  expect("V1 7 OK ASSERT PA7 ASSERTED 10\n");
  assert(low == 128);
  HR_Poll(4);
  assert(low);
  HR_Poll(5);
  assert(!low);
  feed("V1 8 RESET PA8 100\nV1 9 RESET PA0 4294967296\n", 10);
  expect("V1 8 ERR INVALID_PIN\n");
  expect("V1 9 ERR INVALID_ARGUMENT\n");
  assert(!low);
  feed("V1 10 RESET PA0 0\nV1 11 RESET PA0 01\n", 10);
  expect("V1 10 ERR INVALID_ARGUMENT\n");
  expect("V1 11 ERR INVALID_ARGUMENT\n");
  feed("V1 12  INFO\nV2 13 INFO\n", 10);
  expect("V1 12 ERR BAD_FRAME\n");
  expect("V1 13 ERR UNSUPPORTED_VERSION\n");
  feed("V1 14", 20);
  HR_Poll(1020);
  expect("V1 0 ERR FRAME_TIMEOUT\n");
  feed(" RESET PA0 100\n", 1020);
  assert(!low);
  char huge[260];
  memset(huge, 'A', 259);
  huge[259] = 0;
  feed(huge, 1100);
  expect("V1 0 ERR LINE_TOO_LONG\n");
  feed("\nV1 15 STATUS PA0\n", 1100);
  expect("V1 15 OK STATUS PA0 RELEASED 0\n");
  /* Every split point must preserve semantics and duplicate safety. */
  const char *r = "V1 101 RESET PA2 1\r\n";
  for (size_t split = 0; split <= strlen(r); split++) {
    HR_ResetSession();
    feed("V1 100 HELLO 1\n", 0);
    HR_TxPop();
    HR_Feed((const uint8_t *)r, split, 1);
    HR_Feed((const uint8_t *)r + split, strlen(r) - split, 1);
    assert(low == 4);
    HR_Poll(2);
    assert(!low);
    expect("V1 101 OK RESET PA2 RELEASED 1\n");
  }
  HR_ResetSession();
  feed("V1 20 HELLO 1\n", 0);
  HR_TxPop();
  feed("V1 21 RESET PA1 100\n", 0);
  HR_RxOverflow();
  assert(low == 2);
  HR_TxPop();
  feed("V1 22 RELEASE PA1\n", 1);
  assert(low == 2);
  HR_Poll(100);
  assert(!low);
  HR_TxPop();
  /* TX overload must fail closed, release outputs and invalidate handshake. */
  feed("V1 23 ASSERT PA0 100\n", 101);
  HR_TxPop();
  assert(low == 1);
  feed("V1 24 INFO\nV1 25 INFO\nV1 26 INFO\nV1 27 INFO\nV1 28 INFO\n", 102);
  assert(!low);
  feed("V1 29 RESET PA0 100\n", 103);
  expect("V1 29 ERR HANDSHAKE_REQUIRED\n");
  HR_ResetSession();
  char boundary[257];
  memcpy(boundary, "V1 50 ", 6);
  memset(boundary + 6, 'X', 249);
  boundary[255] = '\n';
  boundary[256] = 0;
  feed(boundary, 0);
  expect("V1 50 ERR UNKNOWN_COMMAND\n");
  const uint8_t nul_frame[] = "V1 51 RESET PA0 100\0\n";
  HR_Feed(nul_frame, sizeof nul_frame - 1, 0);
  expect("V1 0 ERR BAD_FRAME\n");
  assert(!low);
  /* Random malformed bytes without a handshake must never assert a pin. */
  uint32_t seed = 12345;
  for (unsigned i = 0; i < 10000; i++) {
    seed = seed * 1664525U + 1013904223U;
    uint8_t byte = (uint8_t)(seed >> 24);
    HR_Feed(&byte, 1, i);
    while (HR_TxPeek())
      HR_TxPop();
    assert(!low);
  }
  puts("protocol tests passed");
  return 0;
}
