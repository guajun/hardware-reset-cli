#include "reset_protocol.h"
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <string.h>

/* All functions are serialized by the adapter. No allocation or blocking IO. */
typedef struct {
  uint32_t id;
  char request[256], response[256];
} Cache;
static Cache cache[8];
static unsigned cache_next;
static char tx[HR_TX_SLOTS][256], line[256], serial_number[33];
static unsigned tx_head, tx_count, used;
static bool discard, handshake, active, pulse;
static uint32_t started, duration, active_id, line_started;
static unsigned active_pin;
static char active_request[256];
static HR_Gpio set_gpio;

void HR_ResetSession(void) {
  if (set_gpio)
    for (unsigned i = 0; i < 8; i++)
      set_gpio(i, 0);
  memset(cache, 0, sizeof cache);
  cache_next = 0;
  tx_head = tx_count = used = 0;
  discard = handshake = active = false;
  active_id = 0;
}
void HR_Init(const char *serial, HR_Gpio gpio) {
  set_gpio = gpio;
  snprintf(serial_number, sizeof serial_number, "%.32s", serial);
  HR_ResetSession();
}
const char *HR_TxPeek(void) {
  return tx_count ? tx[tx_head] : NULL;
}
void HR_TxPop(void) {
  if (tx_count) {
    tx_head = (tx_head + 1) % HR_TX_SLOTS;
    tx_count--;
  }
}
static bool emit(const char *s) {
  if (tx_count == HR_TX_SLOTS) {
    HR_ResetSession();
    return false;
  }
  strcpy(tx[(tx_head + tx_count) % HR_TX_SLOTS], s);
  tx_count++;
  return true;
}
static void result(uint32_t id, const char *req, const char *fmt, ...) {
  char body[208], response[256];
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(body, sizeof body, fmt, ap);
  va_end(ap);
  snprintf(response, sizeof response, "V1 %lu %s\n", (unsigned long)id, body);
  if (!emit(response))
    return;
  if (req && id) {
    Cache *c = &cache[cache_next++ % 8];
    c->id = id;
    strcpy(c->request, req);
    strcpy(c->response, response);
  }
}
static bool number(const char *s, uint32_t *n) {
  if (!*s || (*s == '0' && s[1]))
    return false;
  uint32_t v = 0;
  for (; *s; s++) {
    if (*s < '0' || *s > '9')
      return false;
    unsigned d = (unsigned)(*s - '0');
    if (v > (UINT32_MAX - d) / 10)
      return false;
    v = v * 10 + d;
  }
  *n = v;
  return true;
}
static int pin_number(const char *s) {
  return strlen(s) == 3 && s[0] == 'P' && s[1] == 'A' && s[2] >= '0' && s[2] <= '7' ? s[2] - '0'
                                                                                    : -1;
}
void HR_Poll(uint32_t now) {
  if (active && (uint32_t)(now - started) >= duration) {
    set_gpio(active_pin, 0);
    active = false;
    if (pulse)
      result(active_id, active_request, "OK RESET PA%u RELEASED %lu", active_pin,
             (unsigned long)duration);
  }
  if (used && (uint32_t)(now - line_started) >= 1000) {
    used = 0;
    discard = true;
    result(0, NULL, "ERR FRAME_TIMEOUT");
  }
}
static void command(char *request, uint32_t now) {
  char parsed[256];
  strcpy(parsed, request);
  char *t[7];
  unsigned count = 0;
  bool bad = false;
  t[count++] = parsed;
  for (char *s = parsed; *s; s++) {
    if (*s == ' ') {
      *s = 0;
      if (!s[1] || s[1] == ' ' || count == 7) {
        bad = true;
        break;
      }
      t[count++] = s + 1;
    } else if ((unsigned char)*s < 33 || (unsigned char)*s > 126) {
      bad = true;
      break;
    }
  }
  uint32_t id = 0;
  if (count > 1 && !number(t[1], &id))
    id = 0;
  if (bad || count < 3 || !id || !t[0][0]) {
    result(id, NULL, "ERR BAD_FRAME");
    return;
  }
  if (strcmp(t[0], "V1")) {
    result(id, NULL, "ERR UNSUPPORTED_VERSION");
    return;
  }
  for (unsigned i = 0; i < 8; i++)
    if (cache[i].id == id) {
      if (!strcmp(cache[i].request, request))
        emit(cache[i].response);
      else
        result(id, NULL, "ERR ID_CONFLICT");
      return;
    }
  if (active && active_id == id) {
    if (strcmp(active_request, request))
      result(id, NULL, "ERR ID_CONFLICT");
    return;
  }
  const char *cmd = t[2];
  if (!strcmp(cmd, "HELLO")) {
    if (count != 4) {
      result(id, request, "ERR BAD_FRAME");
      return;
    }
    if (strcmp(t[3], "1")) {
      result(id, request, "ERR UNSUPPORTED_VERSION");
      return;
    }
    handshake = true;
    result(id, request, "OK HELLO HWRESET 1");
    return;
  }
  if (!strcmp(cmd, "INFO")) {
    if (count != 3) {
      result(id, request, "ERR BAD_FRAME");
      return;
    }
    result(id, request, "OK INFO 0.1.0 %s 1 10000 PA0,PA1,PA2,PA3,PA4,PA5,PA6,PA7", serial_number);
    return;
  }
  bool status = !strcmp(cmd, "STATUS"), release = !strcmp(cmd, "RELEASE"),
       reset = !strcmp(cmd, "RESET"), assertion = !strcmp(cmd, "ASSERT");
  if (!status && !release && !reset && !assertion) {
    result(id, request, "ERR UNKNOWN_COMMAND");
    return;
  }
  if (count != ((reset || assertion) ? 5U : 4U)) {
    result(id, request, "ERR BAD_FRAME");
    return;
  }
  int pin = pin_number(t[3]);
  if (pin < 0) {
    result(id, request, "ERR INVALID_PIN");
    return;
  }
  uint32_t ms = 0;
  if ((reset || assertion) && (!number(t[4], &ms) || ms < 1 || ms > 10000)) {
    result(id, request, "ERR INVALID_ARGUMENT");
    return;
  }
  if (status) {
    bool low = active && active_pin == (unsigned)pin;
    result(id, request, "OK STATUS PA%d %s %lu", pin, low ? "ASSERTED" : "RELEASED",
           (unsigned long)(low ? duration - (uint32_t)(now - started) : 0));
    return;
  }
  if (!handshake) {
    result(id, request, "ERR HANDSHAKE_REQUIRED");
    return;
  }
  if (release) {
    set_gpio((unsigned)pin, 0);
    if (active && active_pin == (unsigned)pin) {
      active = false;
      if (pulse)
        result(active_id, active_request, "ERR CANCELLED");
    }
    result(id, request, "OK RELEASE PA%d RELEASED", pin);
    return;
  }
  if (active) {
    result(id, request, "ERR BUSY");
    return;
  }
  /* Keep two slots free for completion and emergency RELEASE, beyond this ACK. */
  if (tx_count > 1) {
    result(id, request, "ERR BUSY");
    return;
  }
  active = true;
  pulse = reset;
  active_pin = (unsigned)pin;
  started = now;
  duration = ms;
  active_id = id;
  strcpy(active_request, request);
  set_gpio(active_pin, 1);
  if (assertion)
    result(id, request, "OK ASSERT PA%d ASSERTED %lu", pin, (unsigned long)ms);
}
void HR_RxOverflow(void) {
  used = 0;
  discard = true;
  result(0, NULL, "ERR BAD_FRAME");
}
void HR_Feed(const uint8_t *data, size_t length, uint32_t now) {
  HR_Poll(now);
  for (size_t i = 0; i < length; i++) {
    uint8_t c = data[i];
    if (discard) {
      if (c == '\n')
        discard = false;
      continue;
    }
    if (!used)
      line_started = now;
    if (c == '\n') {
      if (used && line[used - 1] == '\r')
        used--;
      if (used) {
        line[used] = 0;
        command(line, now);
      }
      used = 0;
    } else if (used >= 255) {
      used = 0;
      discard = true;
      result(0, NULL, "ERR LINE_TOO_LONG");
    } else if (c == 0) {
      used = 0;
      discard = true;
      result(0, NULL, "ERR BAD_FRAME");
    } else
      line[used++] = (char)c;
  }
}
