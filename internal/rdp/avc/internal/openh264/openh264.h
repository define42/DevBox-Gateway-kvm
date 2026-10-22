#ifndef DEVBOX_KVM_OPENH264_H
#define DEVBOX_KVM_OPENH264_H

#include <stddef.h>
#include <stdint.h>

typedef struct DevBoxKVMOpenH264 DevBoxKVMOpenH264;

typedef struct {
    int width;
    int height;
    int frame_rate;
    int qp;
    int slices;
    unsigned int intra_period;
    int loop_filter_disable_idc;
} DevBoxKVMOpenH264Config;

typedef struct {
    int threads;
    int slices;
    unsigned int intra_period;
    int loop_filter_disable_idc;
    float max_frame_rate;
    float layer_frame_rate;
    int layer_qp;
    unsigned int average_frame_qp;
    unsigned int input_frames;
} DevBoxKVMOpenH264Settings;

DevBoxKVMOpenH264 *devbox_kvm_openh264_new(const DevBoxKVMOpenH264Config *config, int *slices,
                                           char *detail, size_t detail_size);
// Encodes one I420 frame and returns the access unit size, or a negative
// OpenH264 status. The bytes stay valid until the next encode or free.
int devbox_kvm_openh264_encode(DevBoxKVMOpenH264 *codec, const uint8_t *i420, int force_idr,
                               int64_t timestamp);
void devbox_kvm_openh264_copy(const DevBoxKVMOpenH264 *codec, uint8_t *dst);
int devbox_kvm_openh264_settings(DevBoxKVMOpenH264 *codec, DevBoxKVMOpenH264Settings *settings);
void devbox_kvm_openh264_free(DevBoxKVMOpenH264 *codec);

#endif
