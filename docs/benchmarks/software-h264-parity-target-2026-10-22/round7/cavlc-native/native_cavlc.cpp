#include "set_mb_syn_cavlc.h"
#include "golomb_common.h"
#include "cpu.h"

#include <array>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <limits>
#include <regex>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

namespace {
constexpr size_t kBufferSize = 4096;
volatile uint64_t sink = 0;

// Native's SSE4.2 extractor requires aligned 16-word inputs and zero padding
// beyond endIdx. The manifest loader enforces that domain before any calls.
struct alignas(16) Block {
  alignas(16) std::array<int16_t, 16> coeff{};
  int32_t end = 0;
  int32_t property = 0;
  int32_t flag = 0;
  int8_t nc = 0;
  int32_t expected = 0;
};
static_assert(sizeof(Block) == 64, "Go/native fixture stride must match");
struct Suite {
  std::string name;
  int32_t prefix = 0;
  uint32_t prefix_value = 0;
  bool timed = false;
  std::vector<Block> blocks;
};

std::vector<Suite> Load(const char* path) {
  std::ifstream input(path);
  std::string magic;
  size_t suite_count = 0;
  if (!(input >> magic >> suite_count) || magic != "cavlc-native-v1" || suite_count > 10000)
    throw std::runtime_error("invalid fixture header");
  std::vector<Suite> suites;
  suites.reserve(suite_count);
  for (size_t si = 0; si < suite_count; ++si) {
    Suite suite;
    int timed = 0;
    size_t block_count = 0;
    if (!(input >> suite.name >> suite.prefix >> suite.prefix_value >> timed >> block_count) ||
        suite.prefix < 0 || suite.prefix > 31 ||
        uint64_t(suite.prefix_value) >= (uint64_t(1) << suite.prefix) ||
        (timed != 0 && timed != 1) || block_count == 0 || block_count > 32)
      throw std::runtime_error("invalid fixture suite");
    suite.timed = timed != 0;
    suite.blocks.resize(block_count);
    for (size_t bi = 0; bi < block_count; ++bi) {
      Block& block = suite.blocks[bi];
      int nc = 0;
      if (!(input >> block.end >> block.property >> nc >> block.flag >> block.expected) ||
          (block.end != 3 && block.end != 14 && block.end != 15) ||
          nc < 0 || nc > 17 || (block.flag != 0 && block.flag != 1) ||
          (block.expected != 0 && block.expected != 64) ||
          (block.expected != 0 && (suite.timed || bi + 1 != block_count)))
        throw std::runtime_error("invalid fixture block metadata");
      if ((block.end == 3 && (block.property != WelsEnc::CHROMA_DC || nc != 17)) ||
          (block.end != 3 && (block.property == WelsEnc::CHROMA_DC || nc > 16)))
        throw std::runtime_error("invalid native nC/property combination");
      block.nc = static_cast<int8_t>(nc);
      for (size_t i = 0; i < block.coeff.size(); ++i) {
        int value = 0;
        if (!(input >> value) || value < -32768 || value > 32767 ||
            ((i > size_t(block.end) || block.flag == 0) && value != 0))
          throw std::runtime_error("invalid native coefficient/padding");
        block.coeff[i] = static_cast<int16_t>(value);
      }
      if (reinterpret_cast<uintptr_t>(block.coeff.data()) % 16 != 0)
        throw std::runtime_error("native coefficient alignment failure");
    }
    suites.push_back(std::move(suite));
  }
  std::string extra;
  if (input >> extra)
    throw std::runtime_error("unexpected trailing fixture data");
  return suites;
}

void Initialize(WelsCommon::SBitStringAux& bits, uint8_t* buffer, const Suite& suite) {
  WelsCommon::InitBits(&bits, buffer, kBufferSize);
  bits.uiCurBits = suite.prefix_value;
  bits.iLeftBits = 32 - suite.prefix;
  bits.iBits = 0x1234;
  bits.iIndex = -17;
}

int32_t EncodeSequence(WelsEnc::SWelsFuncPtrList& functions, Suite& suite,
                       std::array<uint8_t, kBufferSize>& buffer, WelsCommon::SBitStringAux& bits) {
  Initialize(bits, buffer.data(), suite);
  for (Block& block : suite.blocks) {
    int32_t result = WelsEnc::WriteBlockResidualCavlc(&functions, block.coeff.data(), block.end,
        block.flag, block.property, block.nc, &bits);
    if (result != 0)
      return result;
  }
  return 0;
}

void Write32(uint32_t value) {
  uint8_t bytes[4];
  for (unsigned i = 0; i < 4; ++i) bytes[i] = uint8_t(value >> (i * 8));
  if (std::fwrite(bytes, 1, sizeof(bytes), stdout) != sizeof(bytes))
    throw std::runtime_error("write failed");
}
void Write64(uint64_t value) {
  Write32(uint32_t(value));
  Write32(uint32_t(value >> 32));
}
void Dump(WelsEnc::SWelsFuncPtrList& functions, std::vector<Suite>& suites) {
  if (std::fwrite("CAVLCD01", 1, 8, stdout) != 8) throw std::runtime_error("write failed");
  Write32(kBufferSize);
  Write32(static_cast<uint32_t>(suites.size()));
  for (size_t si = 0; si < suites.size(); ++si) {
    Suite& suite = suites[si];
    std::array<uint8_t, kBufferSize> buffer;
    buffer.fill(0xa5);
    WelsCommon::SBitStringAux bits{};
    Initialize(bits, buffer.data(), suite);
    for (size_t bi = 0; bi < suite.blocks.size(); ++bi) {
      Block& block = suite.blocks[bi];
      const auto before = block.coeff;
      int32_t result = WelsEnc::WriteBlockResidualCavlc(&functions, block.coeff.data(), block.end,
          block.flag, block.property, block.nc, &bits);
      if (result != block.expected || block.coeff != before)
        throw std::runtime_error("unexpected return or input mutation in " + suite.name);
      Write32(static_cast<uint32_t>(si));
      Write32(static_cast<uint32_t>(bi));
      Write32(static_cast<uint32_t>(result));
      Write32(static_cast<uint32_t>(bits.pCurBuf - buffer.data()));
      Write32(bits.uiCurBits);
      Write32(static_cast<uint32_t>(bits.iLeftBits));
      Write32(static_cast<uint32_t>(bits.pStartBuf - buffer.data()));
      Write32(static_cast<uint32_t>(bits.pEndBuf - buffer.data()));
      Write32(static_cast<uint32_t>(bits.iBits));
      Write64(static_cast<uint64_t>(bits.iIndex));
      if (std::fwrite(buffer.data(), 1, buffer.size(), stdout) != buffer.size())
        throw std::runtime_error("write failed");
    }
  }
}

std::string CPUName() {
  std::ifstream input("/proc/cpuinfo");
  for (std::string line; std::getline(input, line);) {
    if (line.rfind("model name", 0) == 0) {
      const size_t begin = line.find_first_not_of(" \t", line.find(':') + 1);
      return line.substr(begin);
    }
  }
  return "unknown";
}

void Benchmark(WelsEnc::SWelsFuncPtrList& functions, std::vector<Suite>& suites,
               double milliseconds, int count, const std::regex& filter) {
  std::printf("goos: linux\ngoarch: amd64\npkg: github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/encoder\ncpu: %s\n", CPUName().c_str());
  for (Suite& suite : suites) {
    if (!suite.timed || !std::regex_search(suite.name, filter)) continue;
    std::array<uint8_t, kBufferSize> buffer{};
    WelsCommon::SBitStringAux bits{};
    auto run = [&](uint64_t iterations) {
      const auto start = std::chrono::steady_clock::now();
      for (uint64_t i = 0; i < iterations; ++i) {
        if (EncodeSequence(functions, suite, buffer, bits) != 0)
          throw std::runtime_error("benchmark fixture overflowed: " + suite.name);
      }
      const auto stop = std::chrono::steady_clock::now();
      sink = uint64_t(bits.uiCurBits) + uint64_t(bits.pCurBuf - buffer.data()) + uint64_t(bits.iLeftBits);
      return std::chrono::duration<double, std::nano>(stop - start).count();
    };
    uint64_t iterations = 1;
    while (run(iterations) < milliseconds * 1e6) {
      if (iterations > std::numeric_limits<uint64_t>::max() / 2)
        throw std::runtime_error("iteration count overflow");
      iterations *= 2;
    }
    for (int sample = 0; sample < count; ++sample) {
      const double elapsed = run(iterations);
      const double ns_per_op = elapsed / double(iterations);
      std::printf("BenchmarkCavlcNativeFull/%s %llu %.3f ns/op %.3f ns/block %zu blocks/op 0 B/op 0 allocs/op\n",
          suite.name.c_str(), static_cast<unsigned long long>(iterations), ns_per_op,
          ns_per_op / suite.blocks.size(), suite.blocks.size());
    }
  }
}
}  // namespace

int main(int argc, char** argv) {
  try {
    if (argc < 4 || argc > 7)
      throw std::runtime_error("usage: native_cavlc dump|bench FIXTURES auto|scalar [MILLISECONDS=400] [COUNT=8] [REGEX=.*]");
    const std::string action = argv[1];
    const std::string mode = argv[3];
    if ((action != "dump" && action != "bench") || (mode != "auto" && mode != "scalar"))
      throw std::runtime_error("invalid action or extraction mode");
    auto suites = Load(argv[2]);
    int32_t processors = 0;
    const uint32_t detected = WelsCPUFeatureDetect(&processors);
    WelsEnc::SWelsFuncPtrList functions{};
    WelsEnc::InitCoeffFunc(&functions, mode == "auto" ? detected : 0, 0);
    std::fprintf(stderr, "native_cpu_flags=%#x extraction=%s processors=%d suites=%zu\n",
        detected, functions.pfCavlcParamCal == WelsEnc::CavlcParamCal_c ? "scalar" : "native-selected", processors, suites.size());
    if (action == "dump") {
      Dump(functions, suites);
    } else {
      const double milliseconds = argc > 4 ? std::stod(argv[4]) : 400.0;
      const int count = argc > 5 ? std::stoi(argv[5]) : 8;
      if (milliseconds <= 0 || count <= 0) throw std::runtime_error("invalid duration/count");
      Benchmark(functions, suites, milliseconds, count, std::regex(argc > 6 ? argv[6] : ".*"));
    }
    return 0;
  } catch (const std::exception& error) {
    std::fprintf(stderr, "%s\n", error.what());
    return 1;
  }
}
