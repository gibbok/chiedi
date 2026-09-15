#ifndef CHIEDI_NATIVE_H
#define CHIEDI_NATIVE_H
#include <stdint.h>
#include <stddef.h>
typedef struct ChiediEngine ChiediEngine;
ChiediEngine *chiedi_open(const char *library, const char *model, const char *tokenizer, char **error);
void chiedi_close(ChiediEngine *engine);
int chiedi_tokens(ChiediEngine *engine, const char *text, int64_t **ids, size_t *len, char **error);
int chiedi_run(ChiediEngine *engine, const int64_t *ids, size_t len, float *out, char **error);
#endif
