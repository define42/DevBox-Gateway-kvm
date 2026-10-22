//go:build cgo && !purego

#include "openh264.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "wels/codec_api.h"

struct DevBoxKVMOpenH264 {
    ISVCEncoder *encoder;
    int width;
    int height;
    SFrameBSInfo info;
};

static void fail(char *detail, size_t size, const char *action, int status) {
    if (size > 0) {
        snprintf(detail, size, "%s returned %d", action, status);
    }
}

DevBoxKVMOpenH264 *devbox_kvm_openh264_new(const DevBoxKVMOpenH264Config *config, int *slices,
                                           char *detail, size_t detail_size) {
    DevBoxKVMOpenH264 *codec = calloc(1, sizeof(*codec));
    if (codec == NULL) {
        fail(detail, detail_size, "calloc", -1);
        return NULL;
    }
    codec->width = config->width;
    codec->height = config->height;
    int status = WelsCreateSVCEncoder(&codec->encoder);
    if (status != 0 || codec->encoder == NULL) {
        fail(detail, detail_size, "WelsCreateSVCEncoder", status);
        free(codec);
        return NULL;
    }
    ISVCEncoder *encoder = codec->encoder;

    SEncParamExt param;
    memset(&param, 0, sizeof(param));
    if ((status = (*encoder)->GetDefaultParams(encoder, &param)) != 0) {
        fail(detail, detail_size, "GetDefaultParams", status);
        devbox_kvm_openh264_free(codec);
        return NULL;
    }
    // Keep these settings identical to the pure-Go backend so both produce
    // the same Baseline/CAVLC stream for full-picture encodes.
    param.iPicWidth = config->width;
    param.iPicHeight = config->height;
    param.iTargetBitrate = UNSPECIFIED_BIT_RATE;
    param.fMaxFrameRate = (float)config->frame_rate;
    param.iSpatialLayerNum = 1;
    param.iUsageType = CAMERA_VIDEO_REAL_TIME;
    param.uiIntraPeriod = config->intra_period;
    param.iTemporalLayerNum = 1;
    param.iNumRefFrame = 1;
    param.iRCMode = RC_OFF_MODE;
    param.iLoopFilterDisableIdc = config->loop_filter_disable_idc;
    param.iMultipleThreadIdc = (unsigned short)config->slices;
    param.bUseLoadBalancing = false;
    param.bEnableFrameSkip = false;
    param.bEnableSceneChangeDetect = false;
    param.bEnableBackgroundDetection = false;
    param.bEnableAdaptiveQuant = false;
    param.bEnableLongTermReference = false;
    param.iEntropyCodingModeFlag = 0;
    param.eSpsPpsIdStrategy = CONSTANT_ID;

    SSpatialLayerConfig *layer = &param.sSpatialLayers[0];
    layer->iVideoWidth = config->width;
    layer->iVideoHeight = config->height;
    layer->fFrameRate = (float)config->frame_rate;
    layer->iSpatialBitrate = UNSPECIFIED_BIT_RATE;
    layer->iDLayerQp = config->qp;
    layer->uiProfileIdc = PRO_BASELINE;
    layer->sSliceArgument.uiSliceMode = config->slices > 1 ? SM_FIXEDSLCNUM_SLICE : SM_SINGLE_SLICE;
    layer->sSliceArgument.uiSliceNum = (unsigned int)config->slices;
    // MS-RDPEGFX 3.3.8.3.1 requires full-range BT.709.
    layer->bVideoSignalTypePresent = true;
    layer->uiVideoFormat = VF_UNDEF;
    layer->bFullRange = true;
    layer->bColorDescriptionPresent = true;
    layer->uiColorPrimaries = CP_BT709;
    layer->uiTransferCharacteristics = TRC_BT709;
    layer->uiColorMatrix = CM_BT709;

    int trace = WELS_LOG_QUIET;
    (*encoder)->SetOption(encoder, ENCODER_OPTION_TRACE_LEVEL, &trace);
    if ((status = (*encoder)->InitializeExt(encoder, &param)) != 0) {
        fail(detail, detail_size, "InitializeExt", status);
        devbox_kvm_openh264_free(codec);
        return NULL;
    }
    int format = videoFormatI420;
    if ((status = (*encoder)->SetOption(encoder, ENCODER_OPTION_DATAFORMAT, &format)) != 0) {
        fail(detail, detail_size, "SetOption(DATAFORMAT)", status);
        devbox_kvm_openh264_free(codec);
        return NULL;
    }
    SEncParamExt effective;
    memset(&effective, 0, sizeof(effective));
    if ((status = (*encoder)->GetOption(encoder, ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective)) != 0) {
        fail(detail, detail_size, "GetOption(SVC_ENCODE_PARAM_EXT)", status);
        devbox_kvm_openh264_free(codec);
        return NULL;
    }
    *slices = (int)effective.sSpatialLayers[0].sSliceArgument.uiSliceNum;
    return codec;
}

int devbox_kvm_openh264_encode(DevBoxKVMOpenH264 *codec, const uint8_t *i420, int force_idr,
                               int64_t timestamp, const unsigned char *unchanged, int unchanged_count,
                               const SMacroblockMotion *motion, int motion_count) {
    ISVCEncoder *encoder = codec->encoder;
    int status;
    if (force_idr && (status = (*encoder)->ForceIntraFrame(encoder, true)) != 0) {
        return -status;
    }
    const int luma = codec->width * codec->height;
    SSourcePicture picture;
    memset(&picture, 0, sizeof(picture));
    picture.iColorFormat = videoFormatI420;
    picture.iPicWidth = codec->width;
    picture.iPicHeight = codec->height;
    picture.iStride[0] = codec->width;
    picture.iStride[1] = picture.iStride[2] = codec->width / 2;
    // OpenH264 reads but does not modify the source planes.
    picture.pData[0] = (unsigned char *)i420;
    picture.pData[1] = (unsigned char *)i420 + luma;
    picture.pData[2] = (unsigned char *)i420 + luma + luma / 4;
    picture.uiTimeStamp = timestamp;
    picture.pUnchangedMacroblocks = unchanged;
    picture.iUnchangedMacroblockCount = unchanged != NULL ? unchanged_count : 0;
    picture.pMacroblockMotion = motion;
    picture.iMacroblockMotionCount = motion != NULL ? motion_count : 0;
    memset(&codec->info, 0, sizeof(codec->info));
    if ((status = (*encoder)->EncodeFrame(encoder, &picture, &codec->info)) != 0) {
        return status > 0 ? -status : status - 1;
    }
    if (codec->info.eFrameType == videoFrameTypeSkip || codec->info.eFrameType == videoFrameTypeInvalid) {
        return 0;
    }
    int size = 0;
    for (int layer = 0; layer < codec->info.iLayerNum; ++layer) {
        const SLayerBSInfo *data = &codec->info.sLayerInfo[layer];
        for (int nal = 0; nal < data->iNalCount; ++nal) {
            size += data->pNalLengthInByte[nal];
        }
    }
    return size;
}

void devbox_kvm_openh264_copy(const DevBoxKVMOpenH264 *codec, uint8_t *dst) {
    // Concatenate every layer's NAL units, as the pure-Go backend does.
    for (int layer = 0; layer < codec->info.iLayerNum; ++layer) {
        const SLayerBSInfo *data = &codec->info.sLayerInfo[layer];
        int size = 0;
        for (int nal = 0; nal < data->iNalCount; ++nal) {
            size += data->pNalLengthInByte[nal];
        }
        memcpy(dst, data->pBsBuf, (size_t)size);
        dst += size;
    }
}

int devbox_kvm_openh264_settings(DevBoxKVMOpenH264 *codec, DevBoxKVMOpenH264Settings *settings) {
    ISVCEncoder *encoder = codec->encoder;
    SEncParamExt effective;
    SEncoderStatistics statistics;
    memset(&effective, 0, sizeof(effective));
    memset(&statistics, 0, sizeof(statistics));
    int status = (*encoder)->GetOption(encoder, ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective);
    if (status == 0) {
        status = (*encoder)->GetOption(encoder, ENCODER_OPTION_GET_STATISTICS, &statistics);
    }
    if (status != 0) {
        return status;
    }
    settings->threads = (int)effective.iMultipleThreadIdc;
    settings->slices = (int)effective.sSpatialLayers[0].sSliceArgument.uiSliceNum;
    settings->intra_period = effective.uiIntraPeriod;
    settings->loop_filter_disable_idc = effective.iLoopFilterDisableIdc;
    settings->max_frame_rate = effective.fMaxFrameRate;
    settings->layer_frame_rate = effective.sSpatialLayers[0].fFrameRate;
    settings->layer_qp = effective.sSpatialLayers[0].iDLayerQp;
    settings->average_frame_qp = statistics.uiAverageFrameQP;
    settings->input_frames = statistics.uiInputFrameCount;
    return 0;
}

void devbox_kvm_openh264_free(DevBoxKVMOpenH264 *codec) {
    if (codec == NULL) {
        return;
    }
    if (codec->encoder != NULL) {
        (*codec->encoder)->Uninitialize(codec->encoder);
        WelsDestroySVCEncoder(codec->encoder);
    }
    free(codec);
}
