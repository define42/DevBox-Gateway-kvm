#include <initializer_list>
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>

extern "C" int32_t CavlcParamCal_sse42(int16_t*, uint8_t*, int16_t*, int32_t*, int32_t);

struct Fixture {
  const char* name;
  alignas(16) int16_t coefficients[16];
};

int main(int argc, char** argv) {
  const uint64_t iterations = argc > 1 ? strtoull(argv[1], nullptr, 10) : 10000000;
  const Fixture fixtures[] = {
    {"zero", {}},
    {"dc", {7}},
    {"sparse-low", {7, 0, -2, 1}},
    {"sparse-wide", {7, 0, -2, 0, 0, 1, 0, 0, 0, -1, 0, 0, 0, 0, 0, 1}},
    {"medium", {4, 0, 1, 0, 9, 0, 2, 0, -5, 0, 1, 0, 2, 0, -1}},
    {"nearly-dense", {4, -2, 1, 0, 9, -3, 2, 0, -5, 7, 1, 0, 2, 3, -1}},
    {"one-zero", {4, -2, 1, -1, 9, -3, 2, 0, -5, 7, 1, 1, 2, 3, -1, 1}},
    {"dense", {4, -2, 1, -1, 9, -3, 2, 1, -5, 7, 1, 1, 2, 3, -1, 1}},
  };
  uint64_t checksum = 0;
  for (const int end : {3, 14, 15}) {
    for (const auto& fixture : fixtures) {
      alignas(16) int16_t coefficients[16];
      alignas(16) int16_t levels[16] = {};
      alignas(16) uint8_t runs[16] = {};
      int32_t count = 0;
      memcpy(coefficients, fixture.coefficients, sizeof(coefficients));
      // Native assumes coefficients above end are zero. The Go extractor
      // masks them explicitly, so both produce the same defined output.
      for (int i = end + 1; i < 16; ++i) coefficients[i] = 0;
      for (unsigned i = 0; i < 10000; ++i)
        CavlcParamCal_sse42(coefficients, runs, levels, &count, end);
      const auto start = std::chrono::steady_clock::now();
      for (uint64_t i = 0; i < iterations; ++i)
        CavlcParamCal_sse42(coefficients, runs, levels, &count, end);
      const auto elapsed = std::chrono::steady_clock::now() - start;
      checksum += static_cast<uint64_t>(levels[0]) + runs[0] + count;
      const double ns = std::chrono::duration<double, std::nano>(elapsed).count() / iterations;
      printf("BenchmarkCavlcExtraction/end%d/%s/scalar=false-4 %llu %.3f ns/op 0 B/op 0 allocs/op\n", end,
             fixture.name, static_cast<unsigned long long>(iterations), ns);
    }
  }
  fprintf(stderr, "checksum=%llu\n", static_cast<unsigned long long>(checksum));
}
