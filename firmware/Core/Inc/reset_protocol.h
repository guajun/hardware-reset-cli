#ifndef RESET_PROTOCOL_H
#define RESET_PROTOCOL_H
#include <stddef.h>
#include <stdint.h>
#define HR_MAX_LINE 256
#define HR_TX_SLOTS 4
typedef void (*HR_Gpio)(unsigned pin, int asserted);
void HR_Init(const char *serial, HR_Gpio gpio);
void HR_ResetSession(void);
void HR_Feed(const uint8_t *data, size_t length, uint32_t now);
void HR_Poll(uint32_t now);
const char *HR_TxPeek(void);
void HR_TxPop(void);
void HR_RxOverflow(void);
#endif
