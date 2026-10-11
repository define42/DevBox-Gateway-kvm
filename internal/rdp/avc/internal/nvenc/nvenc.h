#ifndef GOKVM_NVENC_H
#define GOKVM_NVENC_H

#include <stddef.h>
#include <stdint.h>

typedef struct GokvmNVENC GokvmNVENC;

GokvmNVENC *gokvm_nvenc_new(int width, int height, int frame_rate, int qp,
                          char *detail, size_t detail_size);
int gokvm_nvenc_encode(GokvmNVENC *encoder, const uint8_t *i420, int force,
                       int64_t timestamp, uint8_t **data, int *size,
                       char *detail, size_t detail_size);
void gokvm_nvenc_free(GokvmNVENC *encoder);

#endif
