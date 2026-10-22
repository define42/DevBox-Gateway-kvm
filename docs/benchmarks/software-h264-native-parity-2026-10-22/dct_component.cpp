#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <vector>
extern "C" void WelsDctFourT4_avx2(int16_t*, uint8_t*, int32_t, uint8_t*, int32_t);
static volatile int16_t sink;
int main(int argc, char** argv) {
  const int stride = argc > 1 ? std::atoi(argv[1]) : 64;
  const int iterations = argc > 2 ? std::atoi(argv[2]) : 10000000;
  if (stride < 8 || stride > 2048 || iterations <= 0) return 2;
  std::vector<uint8_t> a(7*stride+8), b(7*stride+8);
  for (size_t i = 0; i < a.size(); ++i) {
    a[i] = uint8_t(i*29+17);
    b[i] = uint8_t(i*13+71);
  }
  alignas(32) int16_t output[64]{};
  for (int i = 0; i < 100000; ++i)
    WelsDctFourT4_avx2(output, a.data(), stride, b.data(), stride);
  const auto start = std::chrono::steady_clock::now();
  for (int i = 0; i < iterations; ++i)
    WelsDctFourT4_avx2(output, a.data(), stride, b.data(), stride);
  const auto elapsed = std::chrono::duration<double, std::nano>(std::chrono::steady_clock::now()-start).count();
  sink = output[0];
  std::printf("BenchmarkDCTFourAVX2/stride%d/native-AVX2 %d %.4f ns/op\n", stride, iterations, elapsed/iterations);
  // Deterministic coefficient corpus for direct native/Go comparison. xorshift32
  // uses the same seed and wraparound operations as the Go parity test.
  uint32_t state = 0x264ab;
  uint64_t hash = 14695981039346656037ull;
  for (int iteration = 0; iteration < 1024; ++iteration) {
    for (size_t i = 0; i < a.size(); ++i) {
      state ^= state << 13; state ^= state >> 17; state ^= state << 5;
      a[i] = uint8_t(state);
      state ^= state << 13; state ^= state >> 17; state ^= state << 5;
      b[i] = uint8_t(state);
    }
    WelsDctFourT4_avx2(output, a.data(), stride, b.data(), stride);
    for (int i = 0; i < 64; ++i) {
      const auto v = uint16_t(output[i]);
      hash = (hash ^ uint8_t(v)) * 1099511628211ull;
      hash = (hash ^ uint8_t(v >> 8)) * 1099511628211ull;
    }
  }
  std::printf("native-dct-corpus stride=%d fnv64=%016llx\n", stride, (unsigned long long)hash);
}
