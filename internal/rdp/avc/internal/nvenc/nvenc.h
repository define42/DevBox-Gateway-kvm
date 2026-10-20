#ifndef DEVBOX_KVM_NVENC_H
#define DEVBOX_KVM_NVENC_H

#include <stddef.h>
#include <stdint.h>

typedef struct DevBoxKVMNVENC DevBoxKVMNVENC;

DevBoxKVMNVENC *devbox_kvm_nvenc_new(int width, int height, int frame_rate, int qp,
                                   char *detail, size_t detail_size);
int devbox_kvm_nvenc_encode(DevBoxKVMNVENC *encoder, const uint8_t *i420, int force,
                           int64_t timestamp, uint8_t **data, int *size,
                           char *detail, size_t detail_size);
void devbox_kvm_nvenc_free(DevBoxKVMNVENC *encoder);

#endif
