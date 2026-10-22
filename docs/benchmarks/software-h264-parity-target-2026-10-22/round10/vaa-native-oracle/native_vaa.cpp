// Independent output oracle linked to the actual local OpenH264 static library.
// ABI declarations match codec/processing/src/vaacalc/vaacalculation.h:63,104,109.
#include <algorithm>
#include <array>
#include <cstdint>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <limits>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

using Kernel = void (*)(const uint8_t*, const uint8_t*, int32_t, int32_t,
                       int32_t, int32_t*, int32_t*);
extern "C" void VAACalcSad_avx2(const uint8_t*, const uint8_t*, int32_t,
                                int32_t, int32_t, int32_t*, int32_t*);
extern "C" void VAACalcSad_sse2(const uint8_t*, const uint8_t*, int32_t,
                                int32_t, int32_t, int32_t*, int32_t*);
namespace WelsVP {
void VAACalcSad_c(const uint8_t*, const uint8_t*, int32_t, int32_t, int32_t,
                  int32_t*, int32_t*);
}

namespace {
constexpr int32_t kCanary = 0x5a1739c1;
struct Fixture {
  std::string name;
  int32_t width, height, stride, cur_off, ref_off, pattern;
  uint32_t seed;
};

uint8_t Pixel(int x, int y, uint32_t seed, int pattern, uint32_t plane) {
  const uint32_t ux = static_cast<uint32_t>(x);
  const uint32_t uy = static_cast<uint32_t>(y);
  switch (pattern) {
    case 0: plane = 0; break;  // Equal, nonconstant frames.
    case 1: return static_cast<uint8_t>(plane * 255);
    case 2: return static_cast<uint8_t>(((ux ^ uy ^ plane) & 1) * 255);
    case 3: return static_cast<uint8_t>(ux * 13 + uy * 7 +
                                        plane * (ux * 3 + uy * 11 + 29) + seed);
    case 5: {
      const uint32_t quadrant = ((uy >> 3) & 1) * 2 + ((ux >> 3) & 1);
      return plane == 0 ? 0 : static_cast<uint8_t>((ux >> 4) * 19 +
                                                  (uy >> 4) * 37 + quadrant * 53 + 1);
    }
  }
  uint32_t v = seed ^ ((ux + 1) * 0x9e3779b9u) ^ ((uy + 1) * 0x85ebca6bu) ^
               (plane * 0xc2b2ae35u);
  v ^= v >> 16;
  v *= 0x7feb352du;
  v ^= v >> 15;
  v *= 0x846ca68bu;
  v ^= v >> 16;
  return static_cast<uint8_t>(v);
}

struct FreeBytes {
  void operator()(uint8_t* p) const { std::free(p); }
};
struct Plane {
  std::unique_ptr<uint8_t, FreeBytes> bytes;
  size_t length;
  const uint8_t* pixels;
};
Plane MakePlane(const Fixture& f, int off, uint32_t plane) {
  const size_t span = size_t(f.height - 1) * f.stride + f.width;
  const size_t length = 64 + size_t(off) + span + 64;
  void* raw = nullptr;
  if (posix_memalign(&raw, 32, length) != 0)
    throw std::runtime_error("aligned allocation failed");
  Plane p{std::unique_ptr<uint8_t, FreeBytes>(static_cast<uint8_t*>(raw)), length,
          static_cast<uint8_t*>(raw) + 64 + off};
  std::fill_n(p.bytes.get(), p.length, uint8_t{0xa5});
  for (int y = 0; y < f.height; ++y)
    for (int x = 0; x < f.width; ++x)
      p.bytes.get()[64 + off + size_t(y) * f.stride + x] =
          Pixel(x, y, f.seed, f.pattern, plane);
  return p;
}

std::vector<int32_t> Reference(const Fixture& f, const Plane& cur, const Plane& ref) {
  std::vector<int32_t> out(size_t(f.width / 16) * (f.height / 16) * 4, 0);
  for (int y = 0; y < f.height; ++y) {
    for (int x = 0; x < f.width; ++x) {
      const size_t at = size_t(y) * f.stride + x;
      const int delta = int(cur.pixels[at]) - int(ref.pixels[at]);
      const size_t mb = size_t(y / 16) * (f.width / 16) + x / 16;
      const int quadrant = ((y / 8) & 1) * 2 + ((x / 8) & 1);
      out[mb * 4 + quadrant] += delta < 0 ? -delta : delta;
    }
  }
  return out;
}

struct Result {
  int32_t frame;
  std::vector<int32_t> sad;
};
Result CheckKernel(const Fixture& f, const Plane& cur, const Plane& ref,
                   const std::vector<int32_t>& want, int32_t frame,
                   Kernel fn, const char* kernel) {
  std::vector<int32_t> guarded(want.size() + 8, kCanary);
  std::array<int32_t, 3> frame_guard{kCanary, -999, kCanary};
  fn(cur.pixels, ref.pixels, f.width, f.height, f.stride,
     &frame_guard[1], guarded.data() + 4);
  if (frame_guard[1] != frame || frame_guard[0] != kCanary || frame_guard[2] != kCanary ||
      !std::equal(want.begin(), want.end(), guarded.begin() + 4) ||
      !std::all_of(guarded.begin(), guarded.begin() + 4,
                   [](int32_t v) { return v == kCanary; }) ||
      !std::all_of(guarded.end() - 4, guarded.end(),
                   [](int32_t v) { return v == kCanary; }))
    throw std::runtime_error(f.name + ": " + kernel + " output/frame/canary mismatch");
  return {frame_guard[1], std::vector<int32_t>(guarded.begin() + 4, guarded.end() - 4)};
}

std::vector<Fixture> Load(const char* path) {
  std::ifstream in(path);
  std::string magic;
  int count;
  if (!(in >> magic >> count) || magic != "vaa-fixtures-v1" || count <= 0 || count > 1000)
    throw std::runtime_error("invalid fixture header");
  std::vector<Fixture> out;
  for (int i = 0; i < count; ++i) {
    Fixture f{};
    uint64_t seed;
    if (!(in >> f.name >> f.width >> f.height >> f.stride >> f.cur_off >> f.ref_off >> f.pattern >> seed) ||
        f.width < 16 || f.height < 16 || (f.width & 15) || (f.height & 15) ||
        f.width > 4096 || f.height > 4096 || f.stride < f.width || f.stride > 8192 ||
        f.cur_off < 0 || f.cur_off > 64 || f.ref_off < 0 || f.ref_off > 64 ||
        f.pattern < 0 || f.pattern > 5 || seed > UINT32_MAX ||
        int64_t(f.width) * f.height * 255 > std::numeric_limits<int32_t>::max())
      throw std::runtime_error("invalid fixture metadata");
    f.seed = static_cast<uint32_t>(seed);
    out.push_back(f);
  }
  std::string extra;
  if (in >> extra) throw std::runtime_error("trailing fixture metadata");
  return out;
}
}  // namespace

int main(int argc, char** argv) {
  try {
    if (argc != 3) throw std::runtime_error("usage: native-vaa fixtures.txt native.txt");
    __builtin_cpu_init();
    if (!__builtin_cpu_supports("avx2")) throw std::runtime_error("AVX2 unavailable");
    const auto fixtures = Load(argv[1]);
    std::ofstream out(argv[2]);
    if (!out) throw std::runtime_error("cannot open output");
    out << "vaa-native-v1 " << fixtures.size() << '\n';
    size_t sse2_cases = 0;
    for (const Fixture& f : fixtures) {
      auto cur = MakePlane(f, f.cur_off, 0);
      auto ref = MakePlane(f, f.ref_off, 1);
      const std::vector<uint8_t> cur_before(cur.bytes.get(), cur.bytes.get() + cur.length);
      const std::vector<uint8_t> ref_before(ref.bytes.get(), ref.bytes.get() + ref.length);
      const auto want = Reference(f, cur, ref);
      int64_t sum = 0;
      for (int32_t v : want) sum += v;
      const int32_t frame = static_cast<int32_t>(sum);
      CheckKernel(f, cur, ref, want, frame, WelsVP::VAACalcSad_c, "native C");
      const auto native = CheckKernel(f, cur, ref, want, frame, VAACalcSad_avx2, "native AVX2");
      if ((f.stride & 15) == 0 &&
          (reinterpret_cast<uintptr_t>(cur.pixels) & 15) == 0 &&
          (reinterpret_cast<uintptr_t>(ref.pixels) & 15) == 0) {
        CheckKernel(f, cur, ref, want, frame, VAACalcSad_sse2, "native SSE2");
        ++sse2_cases;
      }
      if (!std::equal(cur_before.begin(), cur_before.end(), cur.bytes.get()) ||
          !std::equal(ref_before.begin(), ref_before.end(), ref.bytes.get()))
        throw std::runtime_error(f.name + ": source/canary mutation");
      // Emit actual native AVX2 output after independent parity checks.
      out << f.name << ' ' << f.width << ' ' << f.height << ' ' << f.stride << ' '
          << f.cur_off << ' ' << f.ref_off << ' ' << f.pattern << ' ' << f.seed << ' '
          << native.frame << ' ' << native.sad.size();
      for (int32_t v : native.sad) out << ' ' << v;
      out << '\n';
    }
    out.close();
    if (!out) throw std::runtime_error("output write failed");
    std::cerr << "PASS " << fixtures.size() << " native AVX2 + C fixtures; "
              << sse2_cases << " aligned native SSE2 fixtures\n";
    return 0;
  } catch (const std::exception& e) {
    std::cerr << e.what() << '\n';
    return 1;
  }
}
