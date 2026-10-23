#include "codec_api.h"
#include <algorithm>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <map>
#include <memory>
#include <stdexcept>
#include <string>
#include <time.h>
#include <vector>

static constexpr int frame_rate = 60;
static int width = 1920, height = 1080;

static void check(int code, const char* action) {
  if (code != 0)
    throw std::runtime_error(std::string(action) + ": " + std::to_string(code));
}
static int integer(const char* value) {
  size_t used = 0;
  const int result = std::stoi(value, &used);
  if (value[used]) throw std::runtime_error("invalid integer");
  return result;
}
static int env_int(const char* name, int fallback) {
  const char* value = std::getenv(name);
  return value && *value ? integer(value) : fallback;
}
static std::string env_string(const char* name) {
  const char* value = std::getenv(name);
  return value ? value : "";
}
static int64_t cpu_ns() {
  timespec t{};
  if (clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t) != 0)
    throw std::runtime_error("clock_gettime failed");
  return int64_t(t.tv_sec) * 1000000000 + t.tv_nsec;
}
static std::vector<uint8_t> read_file(const std::string& name) {
  std::ifstream input(name, std::ios::binary);
  if (!input) throw std::runtime_error("open input failed: " + name);
  input.seekg(0, std::ios::end);
  const auto size = input.tellg();
  if (size <= 0) throw std::runtime_error("empty input: " + name);
  input.seekg(0);
  std::vector<uint8_t> data(static_cast<size_t>(size));
  if (!input.read(reinterpret_cast<char*>(data.data()), size))
    throw std::runtime_error("read input failed: " + name);
  return data;
}
struct EncoderDeleter {
  void operator()(ISVCEncoder* encoder) const {
    encoder->Uninitialize();
    WelsDestroySVCEncoder(encoder);
  }
};
static std::vector<uint8_t> encode(ISVCEncoder* encoder, uint8_t* input,
                                   int frame, const uint8_t* hints, int mb_count) {
  // Includes the force-IDR request and owning output copy, as the Go wrapper does.
  if (frame == 0) check(encoder->ForceIntraFrame(true), "force IDR");
  SSourcePicture pic{};
  pic.iColorFormat = videoFormatI420;
  pic.iPicWidth = width;
  pic.iPicHeight = height;
  pic.iStride[0] = width;
  pic.iStride[1] = pic.iStride[2] = width / 2;
  pic.pData[0] = input;
  pic.pData[1] = input + size_t(width) * height;
  pic.pData[2] = input + size_t(width) * height * 5 / 4;
  pic.uiTimeStamp = int64_t(frame) * 1000 / frame_rate;
  pic.pUnchangedMacroblocks = hints;
  pic.iUnchangedMacroblockCount = hints ? mb_count : 0;
  // Motion hints remain null: the Go production wrapper has no matching API.
  SFrameBSInfo info{};
  check(encoder->EncodeFrame(&pic, &info), "EncodeFrame");
  if (info.eFrameType == videoFrameTypeSkip)
    throw std::runtime_error("unexpected skipped frame");
  std::vector<uint8_t> output;
  for (int layer = 0; layer < info.iLayerNum; ++layer) {
    const auto& data = info.sLayerInfo[layer];
    int size = 0;
    for (int nal = 0; nal < data.iNalCount; ++nal)
      size += data.pNalLengthInByte[nal];
    output.insert(output.end(), data.pBsBuf, data.pBsBuf + size);
  }
  if (output.empty()) throw std::runtime_error("empty bitstream");
  return output;
}
using Settings = std::map<std::string, std::string>;
template <typename T> static void add(Settings& values, const char* key, T value) {
  values[key] = std::to_string(value);
}
static void add(Settings& values, const char* key, bool value) {
  values[key] = value ? "true" : "false";
}
static Settings settings(const SEncParamExt& p) {
  const auto& l = p.sSpatialLayers[0];
  Settings values;
#define FIELD(key, value) add(values, key, value)
  FIELD("width", p.iPicWidth); FIELD("height", p.iPicHeight); FIELD("fps", p.fMaxFrameRate);
  FIELD("layer_width", l.iVideoWidth); FIELD("layer_height", l.iVideoHeight); FIELD("layer_fps", l.fFrameRate);
  FIELD("qp", l.iDLayerQp); FIELD("threads", p.iMultipleThreadIdc); FIELD("slices", l.sSliceArgument.uiSliceNum);
  FIELD("slice_mode", l.sSliceArgument.uiSliceMode); FIELD("slice_size_constraint", l.sSliceArgument.uiSliceSizeConstraint);
  FIELD("complexity", p.iComplexityMode); FIELD("usage", p.iUsageType); FIELD("refs", p.iNumRefFrame);
  FIELD("temporal", p.iTemporalLayerNum); FIELD("spatial", p.iSpatialLayerNum); FIELD("rc", p.iRCMode);
  FIELD("target_bitrate", p.iTargetBitrate); FIELD("max_bitrate", p.iMaxBitrate);
  FIELD("layer_bitrate", l.iSpatialBitrate); FIELD("layer_max_bitrate", l.iMaxSpatialBitrate);
  FIELD("min_qp", p.iMinQp); FIELD("max_qp", p.iMaxQp); FIELD("max_nal", p.uiMaxNalSize);
  FIELD("deblock", p.iLoopFilterDisableIdc); FIELD("deblock_alpha", p.iLoopFilterAlphaC0Offset); FIELD("deblock_beta", p.iLoopFilterBetaOffset);
  FIELD("frame_skip", p.bEnableFrameSkip); FIELD("scene", p.bEnableSceneChangeDetect);
  FIELD("background", p.bEnableBackgroundDetection); FIELD("aq", p.bEnableAdaptiveQuant);
  FIELD("ltr", p.bEnableLongTermReference); FIELD("ltr_refs", p.iLTRRefNum); FIELD("ltr_period", p.iLtrMarkPeriod);
  FIELD("denoise", p.bEnableDenoise); FIELD("load_balance", p.bUseLoadBalancing);
  FIELD("entropy", p.iEntropyCodingModeFlag); FIELD("profile", l.uiProfileIdc); FIELD("level", l.uiLevelIdc);
  FIELD("intra", p.uiIntraPeriod); FIELD("sps_strategy", p.eSpsPpsIdStrategy);
  FIELD("prefix_nal", p.bPrefixNalAddingCtrl); FIELD("ssei", p.bEnableSSEI); FIELD("simulcast", p.bSimulcastAVC);
  FIELD("padding", p.iPaddingFlag); FIELD("cropping", p.bEnableFrameCroppingFlag);
  FIELD("lossless_link", p.bIsLosslessLink); FIELD("fix_rc_overshoot", p.bFixRCOverShoot); FIELD("idr_bitrate_ratio", p.iIdrBitrateRatio);
  FIELD("psnr_y", p.bPsnrY); FIELD("psnr_u", p.bPsnrU); FIELD("psnr_v", p.bPsnrV);
  FIELD("video_signal", l.bVideoSignalTypePresent); FIELD("video_format", l.uiVideoFormat);
  FIELD("full_range", l.bFullRange); FIELD("color_description", l.bColorDescriptionPresent);
  FIELD("color_primaries", l.uiColorPrimaries); FIELD("transfer", l.uiTransferCharacteristics); FIELD("matrix", l.uiColorMatrix);
  FIELD("aspect_ratio_present", l.bAspectRatioPresent); FIELD("aspect_ratio", l.eAspectRatio);
  FIELD("aspect_width", l.sAspectRatioExtWidth); FIELD("aspect_height", l.sAspectRatioExtHeight);
#undef FIELD
  return values;
}
int main(int argc, char** argv) {
  try {
    if (argc != 1 && (argc < 3 || argc > 5))
      throw std::runtime_error("usage: native_bench [I420 SLICES [TIMED_FRAMES [OUTPUT.264]]] or common COMPARE_* environment");
    width = env_int("COMPARE_WIDTH", 1920);
    height = env_int("COMPARE_HEIGHT", 1080);
    const int qp = env_int("COMPARE_QP", 20);
    const int slices = argc >= 3 ? integer(argv[2]) : env_int("COMPARE_SLICES", 1);
    const int timed = argc >= 4 ? integer(argv[3]) : env_int("COMPARE_FRAMES", 60);
    const int warmup = env_int("COMPARE_WARMUP", 16);
    if (width < 2 || height < 2 || width % 2 || height % 2 || qp < 0 || qp > 51 ||
        (slices != 1 && slices != 4) || timed <= 0 || warmup < 0)
      throw std::runtime_error("invalid geometry, QP, slices, frame count or warmup");
    const std::string input_name = argc >= 3 ? argv[1] : env_string("COMPARE_I420");
    const std::string stream_name = argc == 5 ? argv[4] : env_string("COMPARE_STREAM");
    const std::string hint_name = env_string("COMPARE_HINTS");
    std::string case_name = env_string("COMPARE_CASE");
    if (case_name.empty()) case_name = "Clip";
    auto frames = read_file(input_name);
    const size_t frame_bytes = size_t(width) * height * 3 / 2;
    if (frames.size() % frame_bytes != 0) throw std::runtime_error("invalid I420 input length");
    const size_t frame_count = frames.size() / frame_bytes;
    const int mb_count = ((width + 15) / 16) * ((height + 15) / 16);
    std::vector<uint8_t> masks;
    if (!hint_name.empty()) {
      masks = read_file(hint_name);
      if (masks.size() != frame_count * mb_count)
        throw std::runtime_error("hint length must be one byte per MB per input frame");
      for (auto value : masks)
        if (value > 1) throw std::runtime_error("hints must be 0/1 bytes");
    }
    ISVCEncoder* raw = nullptr;
    check(WelsCreateSVCEncoder(&raw), "create");
    std::unique_ptr<ISVCEncoder, EncoderDeleter> encoder(raw);
    SEncParamExt p{};
    check(encoder->GetDefaultParams(&p), "defaults");
    // Explicitly mirror production Go defaults plus production AVC overrides.
    p.iPicWidth = width; p.iPicHeight = height;
    p.iTargetBitrate = UNSPECIFIED_BIT_RATE; p.iMaxBitrate = UNSPECIFIED_BIT_RATE;
    p.fMaxFrameRate = frame_rate;
    p.iSpatialLayerNum = 1; p.iTemporalLayerNum = 1;
    p.iUsageType = CAMERA_VIDEO_REAL_TIME; p.iComplexityMode = LOW_COMPLEXITY;
    p.uiIntraPeriod = 0; p.iNumRefFrame = 1; p.iRCMode = RC_OFF_MODE;
    p.iLoopFilterDisableIdc = 1; p.iLoopFilterAlphaC0Offset = 0; p.iLoopFilterBetaOffset = 0;
    p.iMultipleThreadIdc = slices; p.bUseLoadBalancing = false;
    p.bEnableFrameSkip = false; p.bEnableSceneChangeDetect = false;
    p.bEnableBackgroundDetection = false; p.bEnableAdaptiveQuant = false;
    p.bEnableLongTermReference = false; p.bEnableDenoise = false;
    p.iLTRRefNum = 0; p.iLtrMarkPeriod = 30;
    p.iEntropyCodingModeFlag = 0; p.eSpsPpsIdStrategy = CONSTANT_ID;
    p.bPrefixNalAddingCtrl = false; p.bEnableSSEI = false; p.bSimulcastAVC = false;
    p.iPaddingFlag = 0; p.bEnableFrameCroppingFlag = true;
    p.iMinQp = 0; p.iMaxQp = 51; p.uiMaxNalSize = 0;
    p.bIsLosslessLink = false; p.bFixRCOverShoot = true; p.iIdrBitrateRatio = 400;
    p.bPsnrY = false; p.bPsnrU = false; p.bPsnrV = false;
    auto& layer = p.sSpatialLayers[0];
    layer.iVideoWidth = width; layer.iVideoHeight = height; layer.fFrameRate = frame_rate;
    layer.iSpatialBitrate = UNSPECIFIED_BIT_RATE; layer.iMaxSpatialBitrate = UNSPECIFIED_BIT_RATE;
    layer.iDLayerQp = qp; layer.uiProfileIdc = PRO_BASELINE; layer.uiLevelIdc = LEVEL_UNKNOWN;
    layer.sSliceArgument.uiSliceMode = slices == 1 ? SM_SINGLE_SLICE : SM_FIXEDSLCNUM_SLICE;
    layer.sSliceArgument.uiSliceNum = slices; layer.sSliceArgument.uiSliceSizeConstraint = 1500;
    layer.bVideoSignalTypePresent = true; layer.uiVideoFormat = VF_UNDEF;
    layer.bFullRange = true; layer.bColorDescriptionPresent = true;
    layer.uiColorPrimaries = CP_BT709; layer.uiTransferCharacteristics = TRC_BT709; layer.uiColorMatrix = CM_BT709;
    layer.bAspectRatioPresent = false; layer.eAspectRatio = ASP_UNSPECIFIED;
    layer.sAspectRatioExtWidth = 0; layer.sAspectRatioExtHeight = 0;
    int trace = 0;
    check(encoder->SetOption(ENCODER_OPTION_TRACE_LEVEL, &trace), "trace");
    check(encoder->InitializeExt(&p), "initialize");
    int format = videoFormatI420;
    check(encoder->SetOption(ENCODER_OPTION_DATAFORMAT, &format), "format");
    SEncParamExt effective{};
    check(encoder->GetOption(ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, &effective), "effective params");
    const auto& e = effective;
    const auto& l = e.sSpatialLayers[0];
    if (e.iMultipleThreadIdc != slices || l.sSliceArgument.uiSliceNum != unsigned(slices) ||
        l.sSliceArgument.uiSliceMode != (slices == 1 ? SM_SINGLE_SLICE : SM_FIXEDSLCNUM_SLICE) ||
        e.iPicWidth != width || e.iPicHeight != height || e.fMaxFrameRate != frame_rate || l.fFrameRate != frame_rate ||
        l.iDLayerQp != qp || e.iComplexityMode != LOW_COMPLEXITY || e.iUsageType != CAMERA_VIDEO_REAL_TIME ||
        e.iNumRefFrame != 1 || e.iTemporalLayerNum != 1 || e.iSpatialLayerNum != 1 || e.iRCMode != RC_OFF_MODE ||
        e.iLoopFilterDisableIdc != 1 || e.uiIntraPeriod != 0 || e.bEnableFrameSkip || e.bEnableSceneChangeDetect ||
        e.bEnableBackgroundDetection || e.bEnableAdaptiveQuant || e.bEnableLongTermReference || e.bEnableDenoise ||
        e.bUseLoadBalancing || e.iEntropyCodingModeFlag != 0 || e.eSpsPpsIdStrategy != CONSTANT_ID ||
        l.uiProfileIdc != PRO_BASELINE || !l.bFullRange || l.uiColorMatrix != CM_BT709)
      throw std::runtime_error("effective parameters differ from matched policy");
    auto values = settings(effective);
    std::printf("PARAMS {");
    bool first = true;
    for (const auto& value : values) {
      std::printf("%s\"%s\":%s", first ? "" : ",", value.first.c_str(), value.second.c_str());
      first = false;
    }
    std::printf("}\n");
    std::printf("input_frames=%zu hint_frames=%zu macroblocks_per_frame=%d warmup=%d timed_frames=%d capture=%d\n", frame_count, masks.empty() ? size_t(0) : frame_count, mb_count, warmup, timed, !stream_name.empty());
    const auto version = WelsGetCodecVersion();
    std::printf("native_version=%u.%u.%u\n", version.uMajor, version.uMinor, version.uRevision);
    std::vector<std::vector<uint8_t>> saved;
    if (!stream_name.empty()) saved.reserve(warmup + timed);
    std::vector<int64_t> elapsed(timed), cpu_elapsed(timed);
    std::vector<size_t> frame_sizes(timed);
    uint64_t bytes = 0;
    int64_t sum = 0, cpu = 0;
    for (int i = 0; i < warmup + timed; ++i) {
      const size_t index = i % frame_count;
      uint8_t* source = frames.data() + index * frame_bytes;
      const uint8_t* hints = masks.empty() || i == 0 ? nullptr : masks.data() + index * mb_count;
      const int64_t cpu_start = cpu_ns();
      const auto begin = std::chrono::steady_clock::now();
      auto output = encode(encoder.get(), source, i, hints, mb_count);
      const auto end = std::chrono::steady_clock::now();
      const auto cpu_end = cpu_ns();
      if (i >= warmup) {
        const int n = i - warmup;
        elapsed[n] = std::chrono::duration_cast<std::chrono::nanoseconds>(end - begin).count();
        cpu_elapsed[n] = cpu_end - cpu_start;
        frame_sizes[n] = output.size();
        sum += elapsed[n]; cpu += cpu_elapsed[n]; bytes += output.size();
      }
      if (!stream_name.empty()) saved.emplace_back(std::move(output));
    }
    const auto duration_name = env_string("COMPARE_DURATIONS");
    if (!duration_name.empty()) {
      std::ofstream output(duration_name);
      if (!output) throw std::runtime_error("open duration output failed");
      output << "frame,input_frame,wall_ns,cpu_ns,bytes\n";
      for (int n = 0; n < timed; ++n)
        output << warmup + n << ',' << (warmup + n) % frame_count << ',' << elapsed[n] << ',' << cpu_elapsed[n] << ',' << frame_sizes[n] << '\n';
      output.close();
      if (!output) throw std::runtime_error("write duration output failed");
    }
    std::sort(elapsed.begin(), elapsed.end());
    const int64_t p95 = elapsed[(timed * 95 + 99) / 100 - 1];
    std::printf("BenchmarkOpenH264/%s/%dx%d/QP%d/%dSlices-4 %d %.3f ns/op %.3f cpu_ns/frame %.3f bytes/frame %lld p95_ns/frame\n",
      case_name.c_str(), width, height, qp, slices, timed, double(sum) / timed, double(cpu) / timed, double(bytes) / timed, static_cast<long long>(p95));
    std::printf("timed_bytes=%llu warmup=%d timed_frames=%d\n", static_cast<unsigned long long>(bytes), warmup, timed);
    if (!stream_name.empty()) {
      std::ofstream output(stream_name, std::ios::binary);
      if (!output) throw std::runtime_error("open stream output failed");
      for (const auto& au : saved)
        output.write(reinterpret_cast<const char*>(au.data()), au.size());
      output.close();
      if (!output) throw std::runtime_error("write stream output failed");
    }
    return 0;
  } catch (const std::exception& error) {
    std::fprintf(stderr, "%s\n", error.what());
    return 1;
  }
}
