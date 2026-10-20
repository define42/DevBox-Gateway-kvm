//go:build nvenc && cgo

#include "nvenc.h"

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>

#include <libavcodec/avcodec.h>
#include <libavutil/error.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>

struct DevBoxKVMNVENC {
    AVCodecContext *context;
    AVFrame *frame;
    AVPacket *packet;
};

static int fail(char *detail, size_t size, const char *operation, int code) {
    char reason[AV_ERROR_MAX_STRING_SIZE];
    if (av_strerror(code, reason, sizeof(reason)) < 0)
        snprintf(reason, sizeof(reason), "error %d", code);
    snprintf(detail, size, "%s: %s", operation, reason);
    return code;
}

void devbox_kvm_nvenc_free(DevBoxKVMNVENC *encoder) {
    if (!encoder)
        return;
    av_packet_free(&encoder->packet);
    av_frame_free(&encoder->frame);
    avcodec_free_context(&encoder->context);
    av_free(encoder);
}

DevBoxKVMNVENC *devbox_kvm_nvenc_new(int width, int height, int frame_rate, int qp,
                                   char *detail, size_t detail_size) {
    const AVCodec *codec = avcodec_find_encoder_by_name("h264_nvenc");
    if (!codec) {
        snprintf(detail, detail_size, "ffmpeg has no h264_nvenc encoder");
        return NULL;
    }
    DevBoxKVMNVENC *encoder = av_mallocz(sizeof(*encoder));
    if (!encoder) {
        fail(detail, detail_size, "allocate encoder", AVERROR(ENOMEM));
        return NULL;
    }
    encoder->context = avcodec_alloc_context3(codec);
    encoder->frame = av_frame_alloc();
    encoder->packet = av_packet_alloc();
    if (!encoder->context || !encoder->frame || !encoder->packet) {
        fail(detail, detail_size, "allocate codec buffers", AVERROR(ENOMEM));
        goto error;
    }

    AVCodecContext *context = encoder->context;
    context->width = width;
    context->height = height;
    context->pix_fmt = AV_PIX_FMT_YUV420P;
    context->time_base = (AVRational){1, 1000};
    context->framerate = (AVRational){frame_rate, 1};
    context->sample_aspect_ratio = (AVRational){1, 1};
    // Use the largest explicit GOP: zero means all-intra and negative values
    // retain the preset's GOP. RDP explicitly requests refresh IDRs as needed.
    context->gop_size = INT_MAX;
    context->max_b_frames = 0;
    // Leave reference-list selection automatic for pre-Turing GPUs. Setting
    // refs=1 requires their unsupported multiple-reference capability in FFmpeg.
    // Limit the decoded picture buffer to one reference via dpb_size below.
    context->refs = 0;
    context->flags |= AV_CODEC_FLAG_LOW_DELAY;
    // NVENC repeats SPS/PPS at IDRs when GLOBAL_HEADER is clear.
    context->flags &= ~AV_CODEC_FLAG_GLOBAL_HEADER;
    context->color_range = AVCOL_RANGE_JPEG;
    context->colorspace = AVCOL_SPC_BT709;
    context->color_primaries = AVCOL_PRI_BT709;
    context->color_trc = AVCOL_TRC_BT709;
    context->i_quant_factor = 0;
    context->b_quant_factor = 0;

    const char *options[][2] = {
        {"preset", "p1"}, {"tune", "ull"},
        {"profile", "baseline"}, {"coder", "cavlc"},
        {"rc", "constqp"}, {"rc-lookahead", "0"},
        {"zerolatency", "1"}, {"delay", "0"},
        {"forced-idr", "1"}, {"no-scenecut", "1"},
        {"spatial-aq", "0"}, {"temporal-aq", "0"},
        {"multipass", "disabled"}, {"aud", "1"}, {"dpb_size", "1"},
    };
    for (size_t i = 0; i < sizeof(options) / sizeof(options[0]); i++) {
        int result = av_opt_set(context->priv_data, options[i][0], options[i][1], 0);
        if (result < 0) {
            fail(detail, detail_size, options[i][0], result);
            goto error;
        }
    }
    int result = av_opt_set_int(context->priv_data, "qp", qp, 0);
    if (result < 0) {
        fail(detail, detail_size, "set quantizer", result);
        goto error;
    }
    result = avcodec_open2(context, codec, NULL);
    if (result < 0) {
        fail(detail, detail_size, "open h264_nvenc (check nvidia driver and gpu support)", result);
        goto error;
    }

    AVFrame *frame = encoder->frame;
    frame->format = context->pix_fmt;
    frame->width = width;
    frame->height = height;
    frame->color_range = context->color_range;
    frame->colorspace = context->colorspace;
    frame->color_primaries = context->color_primaries;
    frame->color_trc = context->color_trc;
    result = av_frame_get_buffer(frame, 32);
    if (result < 0) {
        fail(detail, detail_size, "allocate i420 frame", result);
        goto error;
    }
    return encoder;

error:
    devbox_kvm_nvenc_free(encoder);
    return NULL;
}

int devbox_kvm_nvenc_encode(DevBoxKVMNVENC *encoder, const uint8_t *i420, int force,
                           int64_t timestamp, uint8_t **data, int *size,
                           char *detail, size_t detail_size) {
    AVFrame *frame = encoder->frame;
    AVPacket *packet = encoder->packet;
    av_packet_unref(packet);
    int result = av_frame_make_writable(frame);
    if (result < 0)
        return fail(detail, detail_size, "prepare i420 frame", result);

    for (int plane = 0; plane < 3; plane++) {
        int width = frame->width >> (plane != 0);
        int height = frame->height >> (plane != 0);
        for (int row = 0; row < height; row++) {
            memcpy(frame->data[plane] + row * frame->linesize[plane], i420, width);
            i420 += width;
        }
    }
    frame->pts = timestamp;
    frame->pict_type = force ? AV_PICTURE_TYPE_I : AV_PICTURE_TYPE_NONE;
    result = avcodec_send_frame(encoder->context, frame);
    if (result < 0)
        return fail(detail, detail_size, "send frame", result);
    result = avcodec_receive_packet(encoder->context, packet);
    if (result < 0)
        return fail(detail, detail_size, "receive synchronous access unit", result);
    if (packet->size <= 0 || packet->pts != timestamp)
        return fail(detail, detail_size, "empty or delayed access unit", AVERROR_INVALIDDATA);

    *data = packet->data;
    *size = packet->size;
    return 0;
}
