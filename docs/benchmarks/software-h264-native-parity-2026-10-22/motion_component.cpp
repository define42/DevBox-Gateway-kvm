#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>
extern "C" int32_t WelsSampleSatd8x8_avx2(uint8_t*, int32_t, uint8_t*, int32_t);
extern "C" int32_t WelsSampleSatd16x8_avx2(uint8_t*, int32_t, uint8_t*, int32_t);
extern "C" int32_t WelsSampleSatd8x16_avx2(uint8_t*, int32_t, uint8_t*, int32_t);
extern "C" int32_t WelsSampleSatd16x16_avx2(uint8_t*, int32_t, uint8_t*, int32_t);
extern "C" void McHorVer20_avx2(uint8_t*, int32_t, uint8_t*, int32_t, int32_t, int32_t);
static volatile int32_t sink;
static uint64_t hash_word(uint64_t hash, uint32_t value) {
  for (int i=0; i<4; ++i) hash = (hash ^ uint8_t(value>>(i*8))) * 1099511628211ull;
  return hash;
}
int main(int argc, char** argv) {
  const int iterations=argc>1?std::atoi(argv[1]):3000000;
  if (iterations<1) return 2;
  uint8_t a[32*16+3], b[32*16+7];
  for (size_t i=0; i<sizeof(a); ++i) a[i]=uint8_t(i*17);
  for (size_t i=0; i<sizeof(b); ++i) b[i]=uint8_t(i*23+3);
  using Fn = int32_t(*)(uint8_t*,int32_t,uint8_t*,int32_t);
  struct Case {const char* name; Fn fn;};
  const Case cases[]={{"8x8",WelsSampleSatd8x8_avx2},{"16x8",WelsSampleSatd16x8_avx2},{"8x16",WelsSampleSatd8x16_avx2},{"16x16",WelsSampleSatd16x16_avx2}};
  for (const auto& c:cases) {
    int32_t result=0;
    for(int i=0;i<100000;++i) result=c.fn(a+3,32,b+7,32);
    auto start=std::chrono::steady_clock::now();
    for(int i=0;i<iterations;++i) result=c.fn(a+3,32,b+7,32);
    const auto elapsed=std::chrono::duration<double,std::nano>(std::chrono::steady_clock::now()-start).count();
    sink=result;
    std::printf("BenchmarkSampleSatdBlocks/%s/avx2 %d %.4f ns/op\n",c.name,iterations,elapsed/iterations);
  }
  alignas(32) uint8_t src[48*32], dst[32*17];
  for(size_t i=0;i<sizeof(src);++i)src[i]=uint8_t(i*29+41);
  for(int i=0;i<100000;++i)McHorVer20_avx2(src+3*48+3,48,dst,32,16,16);
  auto start=std::chrono::steady_clock::now();
  for(int i=0;i<iterations;++i)McHorVer20_avx2(src+3*48+3,48,dst,32,16,16);
  auto elapsed=std::chrono::duration<double,std::nano>(std::chrono::steady_clock::now()-start).count();
  sink=dst[0];
  std::printf("BenchmarkMcAVX2Halfpel/horizontal/16x16/AVX2 %d %.4f ns/op\n",iterations,elapsed/iterations);
  uint32_t state=0x2645ad;
  uint64_t hash=14695981039346656037ull;
  auto next=[&](){state^=state<<13;state^=state>>17;state^=state<<5;return uint8_t(state);};
  for(int i=0;i<1024;++i){
    for(auto& value:a)value=next();
    for(auto& value:b)value=next();
    for(const auto& c:cases) hash=hash_word(hash,uint32_t(c.fn(a+3,32,b+7,32)));
    for(auto& value:src)value=next();
    McHorVer20_avx2(src+3*48+3,48,dst,32,16,16);
    for(int y=0;y<16;++y)for(int x=0;x<16;++x)hash=(hash^dst[y*32+x])*1099511628211ull;
  }
  std::printf("native-motion-corpus fnv64=%016llx\n",(unsigned long long)hash);
}
