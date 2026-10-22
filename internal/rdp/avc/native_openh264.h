//go:build openh264_native && cgo && linux

#ifndef DEVBOX_NATIVE_OPENH264_H
#define DEVBOX_NATIVE_OPENH264_H
#include <stdint.h>
#include <stddef.h>
#ifdef __cplusplus
extern "C" {
#endif
typedef struct devbox_h264 devbox_h264;
devbox_h264 *devbox_h264_new(int width,int height,int fps,int qp,int threads,int *slices,int *error);
int devbox_h264_encode(devbox_h264 *encoder, const uint8_t *i420, int force, uint64_t timestamp,
                       uint8_t **output, size_t *output_size);
void devbox_h264_free(devbox_h264 *encoder);
#ifdef __cplusplus
}
#endif
#endif
