#include <cstdio>
#include "cpu.h"
#include "sample.h"
#include "sad_common.h"
int main() {
  int32_t logical = 0;
  const uint32_t flags = WelsCPUFeatureDetect(&logical);
  WelsEnc::SWelsFuncPtrList f{};
  WelsEnc::WelsInitSampleSadFunc(&f, flags);
  std::printf("cpu_flags=0x%08x logical_cpus=%d sse2=%d sse41=%d avx2=%d\n", flags, logical,
      !!(flags & WELS_CPU_SSE2), !!(flags & WELS_CPU_SSE41), !!(flags & WELS_CPU_AVX2));
  std::printf("satd16x16_avx2_selected=%d sadfour16x16_avx2_selected=%d\n",
      f.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16] == WelsEnc::WelsSampleSatd16x16_avx2,
      f.sSampleDealingFuncs.pfSample4Sad[BLOCK_16x16] == WelsSampleSadFour16x16_avx2);
}
