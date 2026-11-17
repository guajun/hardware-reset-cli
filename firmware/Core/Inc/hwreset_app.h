#ifndef HWRESET_APP_H
#define HWRESET_APP_H
#include <stddef.h>
#include <stdint.h>
void HWRESET_Init(void);
/* Boot only: signal detach for Blue Pill boards with a fixed D+ pull-up. */
void HWRESET_USB_Reenumerate(void);
void HWRESET_Poll(void);
void HWRESET_Receive(const uint8_t *data, size_t length);
void HWRESET_SessionReset(void);
void HWRESET_Serial(char out[25]);
#endif
