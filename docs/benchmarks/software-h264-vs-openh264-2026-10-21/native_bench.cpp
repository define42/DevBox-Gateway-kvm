#include "codec_api.h"
#include <algorithm>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <stdexcept>
#include <string>
#include <time.h>
#include <vector>

static constexpr int width = 1920;
static constexpr int height = 1080;
static constexpr int frame_rate = 60;
static constexpr int warmup = 16;
static constexpr int qp = 20;

static int64_t cpu_ns() {
  timespec t{};
  if (clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t) != 0)
    throw std::runtime_error("clock_gettime failed");
  return int64_t(t.tv_sec) * 1000000000 + t.tv_nsec;
}
static void check(int code, const char* action) {
  if (code != 0)
    throw std::runtime_error(std::string(action) + ": " + std::to_string(code));
}
static std::vector<uint8_t> encode(ISVCEncoder* encoder, uint8_t* input, int frame) {
  SSourcePicture pic{};
  pic.iColorFormat = videoFormatI420;
  pic.iPicWidth = width;
  pic.iPicHeight = height;
  pic.iStride[0] = width;
  pic.iStride[1] = pic.iStride[2] = width / 2;
  pic.pData[0] = input;
  pic.pData[1] = input + width * height;
  pic.pData[2] = input + width * height * 5 / 4;
  pic.uiTimeStamp = int64_t(frame) * 1000 / frame_rate;
  SFrameBSInfo info{};
  check(encoder->EncodeFrame(&pic, &info), "EncodeFrame");
  if (info.eFrameType == videoFrameTypeSkip)
    throw std::runtime_error("unexpected skipped frame");
  std::vector<uint8_t> output;
  // Mirror the Go wrapper: concatenate every layer into a caller-owned buffer.
  for (int layer = 0; layer < info.iLayerNum; ++layer) {
    const auto& data = info.sLayerInfo[layer];
    int size = 0;
    for (int nal = 0; nal < data.iNalCount; ++nal)
      size += data.pNalLengthInByte[nal];
    output.insert(output.end(), data.pBsBuf, data.pBsBuf + size);
  }
  if (output.empty())
    throw std::runtime_error("empty bitstream");
  return output;
}
int main(int argc, char** argv) {
  try {
    if (argc < 3 || argc > 5)
      throw std::runtime_error("usage: native_bench I420 SLICES [TIMED_FRAMES=60] [OUTPUT.264]");
    const int slices = std::stoi(argv[2]);
    const int timed = argc >= 4 ? std::stoi(argv[3]) : 60;
    if ((slices != 1 && slices != 4) || timed <= 0)
      throw std::runtime_error("slices must be 1 or 4; timed frames must be positive");
    std::ifstream input(argv[1], std::ios::binary);
    if (!input)
      throw std::runtime_error("open input failed");
    std::vector<uint8_t> frames((std::istreambuf_iterator<char>(input)), {});
    const size_t frame_bytes = width * height * 3 / 2;
    if (frames.empty() || frames.size() % frame_bytes != 0)
      throw std::runtime_error("invalid I420 input length");
    const size_t frame_count = frames.size() / frame_bytes;
    ISVCEncoder* encoder = nullptr;
    check(WelsCreateSVCEncoder(&encoder), "create");
    SEncParamExt param{};
    check(encoder->GetDefaultParams(&param), "defaults");
    param.iPicWidth = width;
    param.iPicHeight = height;
    param.iTargetBitrate = UNSPECIFIED_BIT_RATE;
    param.fMaxFrameRate = frame_rate;
    param.iSpatialLayerNum = 1;
    param.iUsageType = CAMERA_VIDEO_REAL_TIME;
    param.uiIntraPeriod = 0;
    param.iTemporalLayerNum = 1;
    param.iNumRefFrame = 1;
    param.iRCMode = RC_OFF_MODE;
    param.iLoopFilterDisableIdc = 1;
    param.iMultipleThreadIdc = slices;
    param.bUseLoadBalancing = false;
    param.bEnableFrameSkip = false;
    param.bEnableSceneChangeDetect = false;
    param.bEnableBackgroundDetection = false;
    param.bEnableAdaptiveQuant = false;
    param.bEnableLongTermReference = false;
    param.iEntropyCodingModeFlag = 0;
    param.eSpsPpsIdStrategy = CONSTANT_ID;
    auto& layer = param.sSpatialLayers[0];
    layer.iVideoWidth = width;
    layer.iVideoHeight = height;
    layer.fFrameRate = frame_rate;
    layer.iSpatialBitrate = UNSPECIFIED_BIT_RATE;
    layer.iDLayerQp = qp;
    layer.uiProfileIdc = PRO_BASELINE;
    layer.sSliceArgument.uiSliceMode = slices == 1 ? SM_SINGLE_SLICE : SM_FIXEDSLCNUM_SLICE;
    layer.sSliceArgument.uiSliceNum = slices;
    layer.bVideoSignalTypePresent = true;
    layer.uiVideoFormat = VF_UNDEF;
    layer.bFullRange = true;
    layer.bColorDescriptionPresent = true;
    layer.uiColorPrimaries = CP_BT709;
    layer.uiTransferCharacteristics = TRC_BT709;
    layer.uiColorMatrix = CM_BT709;
    int trace = 0;
    check(encoder->SetOption(ENCODER_OPTION_TRACE_LEVEL, &trace), "trace");
    check(encoder->InitializeExt(&param), "initialize");
    int format = videoFormatI420;
    check(encoder->SetOption(ENCODER_OPTION_DATAFORMAT, &format), "format");
    SEncParamExt effective{};
    check(encoder->GetOption(ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective), "effective params");
    const auto& l = effective.sSpatialLayers[0];
    const auto version = WelsGetCodecVersion();
    std::printf("native_version=%u.%u.%u input_frames=%zu width=%d height=%d fps=%.0f qp=%d "
      "threads=%u slices=%u slice_mode=%d complexity=%d usage=%d refs=%d temporal=%d spatial=%d "
      "rc=%d deblock=%d frame_skip=%d scene=%d background=%d aq=%d ltr=%d denoise=%d "
      "load_balance=%d entropy=%d profile=%d intra=%u sps_strategy=%d full_range=%d matrix=%u\n",
      version.uMajor, version.uMinor, version.uRevision, frame_count,
      effective.iPicWidth, effective.iPicHeight, effective.fMaxFrameRate, l.iDLayerQp,
      effective.iMultipleThreadIdc, l.sSliceArgument.uiSliceNum, l.sSliceArgument.uiSliceMode,
      effective.iComplexityMode, effective.iUsageType, effective.iNumRefFrame,
      effective.iTemporalLayerNum, effective.iSpatialLayerNum, effective.iRCMode,
      effective.iLoopFilterDisableIdc, effective.bEnableFrameSkip, effective.bEnableSceneChangeDetect,
      effective.bEnableBackgroundDetection, effective.bEnableAdaptiveQuant, effective.bEnableLongTermReference,
      effective.bEnableDenoise, effective.bUseLoadBalancing, effective.iEntropyCodingModeFlag,
      l.uiProfileIdc, effective.uiIntraPeriod, effective.eSpsPpsIdStrategy, l.bFullRange, l.uiColorMatrix);
    if (effective.iMultipleThreadIdc != slices || l.sSliceArgument.uiSliceNum != unsigned(slices))
      throw std::runtime_error("effective slicing differs from request");
    std::vector<std::vector<uint8_t>> saved;
    if (argc == 5)
      saved.reserve(warmup + timed);
    check(encoder->ForceIntraFrame(true), "force IDR");
    for (int i = 0; i < warmup; ++i) {
      auto output = encode(encoder, frames.data() + (i % frame_count) * frame_bytes, i);
      if (argc == 5)
        saved.emplace_back(std::move(output));
    }
    std::vector<int64_t> elapsed(timed);
    uint64_t bytes = 0;
    int64_t sum = 0;
    int64_t cpu = 0;
    for (int n = 0; n < timed; ++n) {
      const int i = n + warmup;
      uint8_t* source = frames.data() + (i % frame_count) * frame_bytes;
      const int64_t cpu_start = cpu_ns();
      const auto begin = std::chrono::steady_clock::now();
      auto output = encode(encoder, source, i);
      const auto end = std::chrono::steady_clock::now();
      cpu += cpu_ns() - cpu_start;
      elapsed[n] = std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count();
      sum += elapsed[n];
      bytes += output.size();
      if (argc == 5)
        saved.emplace_back(std::move(output));
    }
    std::sort(elapsed.begin(), elapsed.end());
    const int64_t p95 = elapsed[(timed * 95 + 99) / 100 - 1];
    std::printf("BenchmarkOpenH264/1080p/%dSlices-4 %d %.0f ns/op %.0f cpu_ns/frame %.0f bytes/frame %lld p95_ns/frame\n",
      slices, timed, double(sum) / timed, double(cpu) / timed, double(bytes) / timed, static_cast<long long>(p95));
    std::printf("timed_bytes=%llu warmup=%d timed_frames=%d\n", static_cast<unsigned long long>(bytes), warmup, timed);
    if (argc == 5) {
      std::ofstream output(argv[4], std::ios::binary);
      if (!output)
        throw std::runtime_error("open stream output failed");
      for (const auto& au : saved)
        output.write(reinterpret_cast<const char*>(au.data()), au.size());
      if (!output)
        throw std::runtime_error("write stream output failed");
    }
    encoder->Uninitialize();
    WelsDestroySVCEncoder(encoder);
    return 0;
  } catch (const std::exception& error) {
    std::fprintf(stderr, "%s\n", error.what());
    return 1;
  }
}
