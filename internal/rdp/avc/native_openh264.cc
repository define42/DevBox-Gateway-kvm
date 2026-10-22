//go:build openh264_native && cgo && linux

#include "native_openh264.h"
#include <wels/codec_api.h>
#include <cstdlib>
#include <cstring>
#include <limits>

struct devbox_h264 {
    ISVCEncoder *encoder;
    int width;
    int height;
};

extern "C" devbox_h264 *devbox_h264_new(int width, int height, int fps, int qp, int threads, int *slices, int *error) {
    *error = -1;
    *slices = 0;
    if (width < 16 || height < 16 || (width & 1) || (height & 1) || fps < 1 || qp < 0 || qp > 51 || threads < 1)
        return nullptr;
    auto *handle = static_cast<devbox_h264*>(std::calloc(1, sizeof(devbox_h264)));
    if (!handle) return nullptr;
    if (WelsCreateSVCEncoder(&handle->encoder) != 0 || !handle->encoder) {
        std::free(handle);
        return nullptr;
    }
    SEncParamExt p{};
    int rc = handle->encoder->GetDefaultParams(&p);
    if (rc == 0) {
        p.iPicWidth = width;
        p.iPicHeight = height;
        p.fMaxFrameRate = static_cast<float>(fps);
        p.iUsageType = CAMERA_VIDEO_REAL_TIME;
        p.iRCMode = RC_OFF_MODE;
        p.iSpatialLayerNum = 1;
        p.iTemporalLayerNum = 1;
        p.iNumRefFrame = 1;
        p.uiIntraPeriod = 0;
        p.iMultipleThreadIdc = threads;
        p.bEnableFrameSkip = false;
        p.bEnableSceneChangeDetect = false;
        p.bEnableBackgroundDetection = false;
        p.bEnableAdaptiveQuant = false;
        p.bEnableLongTermReference = false;
        p.iEntropyCodingModeFlag = 0;
        p.iLoopFilterDisableIdc = 1;
        p.eSpsPpsIdStrategy = CONSTANT_ID;
        auto &layer = p.sSpatialLayers[0];
        layer.iVideoWidth = width;
        layer.iVideoHeight = height;
        layer.fFrameRate = static_cast<float>(fps);
        layer.iDLayerQp = qp;
        layer.uiProfileIdc = PRO_BASELINE;
        layer.sSliceArgument.uiSliceMode = threads > 1 ? SM_FIXEDSLCNUM_SLICE : SM_SINGLE_SLICE;
        layer.sSliceArgument.uiSliceNum = static_cast<unsigned int>(threads);
        layer.bVideoSignalTypePresent = true;
        layer.uiVideoFormat = VF_UNDEF;
        layer.bFullRange = true;
        layer.bColorDescriptionPresent = true;
        layer.uiColorPrimaries = CP_BT709;
        layer.uiTransferCharacteristics = TRC_BT709;
        layer.uiColorMatrix = CM_BT709;
        rc = handle->encoder->InitializeExt(&p);
    }
    if (rc != 0) {
        *error = rc;
        WelsDestroySVCEncoder(handle->encoder);
        std::free(handle);
        return nullptr;
    }
    int format = videoFormatI420;
    rc = handle->encoder->SetOption(ENCODER_OPTION_DATAFORMAT, &format);
    if (rc != 0) {
        *error = rc;
        handle->encoder->Uninitialize();
        WelsDestroySVCEncoder(handle->encoder);
        std::free(handle);
        return nullptr;
    }
    SEncParamExt effective{};
    rc = handle->encoder->GetOption(ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective);
    *slices = rc == 0 ? static_cast<int>(effective.sSpatialLayers[0].sSliceArgument.uiSliceNum) : threads;
    if (*slices < 1) *slices = 1;
    handle->width = width;
    handle->height = height;
    *error = 0;
    return handle;
}
extern "C" int devbox_h264_encode(devbox_h264 *handle,const uint8_t *data,int force,uint64_t timestamp,
                                  uint8_t **output,size_t *output_size) {
    if (!handle || !data || !output || !output_size) return -1;
    *output = nullptr;
    *output_size = 0;
    if (force) {
        const int result = handle->encoder->ForceIntraFrame(true);
        if (result != 0) return result;
    }
    SSourcePicture picture{};
    picture.iColorFormat = videoFormatI420;
    picture.iPicWidth = handle->width;
    picture.iPicHeight = handle->height;
    picture.iStride[0] = handle->width;
    picture.iStride[1] = picture.iStride[2] = handle->width / 2;
    const size_t luma = static_cast<size_t>(handle->width) * handle->height;
    picture.pData[0] = const_cast<uint8_t*>(data);
    picture.pData[1] = const_cast<uint8_t*>(data + luma);
    picture.pData[2] = const_cast<uint8_t*>(data + luma + luma / 4);
    picture.uiTimeStamp = timestamp;
    SFrameBSInfo info{};
    int rc = handle->encoder->EncodeFrame(&picture, &info);
    if (rc != 0) return rc;
    if (info.eFrameType == videoFrameTypeSkip || info.iLayerNum <= 0) return -2;
    size_t total = 0;
    for (int layer = 0; layer < info.iLayerNum; layer++) {
        const auto &item = info.sLayerInfo[layer];
        if (!item.pBsBuf || item.iNalCount < 0) return -3;
        for (int nal = 0; nal < item.iNalCount; nal++) {
            if (item.pNalLengthInByte[nal] <= 0) return -4;
            total += static_cast<size_t>(item.pNalLengthInByte[nal]);
        }
    }
    if (!total || total > static_cast<size_t>(std::numeric_limits<int>::max())) return -5;
    auto *copy = static_cast<uint8_t*>(std::malloc(total));
    if (!copy) return -6;
    size_t offset = 0;
    for (int layer = 0; layer < info.iLayerNum; layer++) {
        const auto &item = info.sLayerInfo[layer];
        size_t length = 0;
        for (int nal = 0; nal < item.iNalCount; nal++)
            length += static_cast<size_t>(item.pNalLengthInByte[nal]);
        std::memcpy(copy + offset, item.pBsBuf, length);
        offset += length;
    }
    *output = copy;
    *output_size = total;
    return 0;
}
extern "C" void devbox_h264_free(devbox_h264 *handle) {
    if (!handle) return;
    if (handle->encoder) {
        handle->encoder->Uninitialize();
        WelsDestroySVCEncoder(handle->encoder);
    }
    std::free(handle);
}
