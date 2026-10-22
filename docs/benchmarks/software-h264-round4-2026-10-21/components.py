from pathlib import Path
import re,subprocess
base=Path(__file__).resolve().parent
families={
 "sad": ("micro-common.txt", "BenchmarkSampleSadFourBatched", "/FourSingles", "/Batched"),
 "halfpel": ("micro-common.txt", "BenchmarkMcAVX2", "/SSE2", "/AVX2"),
 "idct": ("micro-encoder.txt", "BenchmarkIDctPairedKernel", "/PriorFourSSE2", "/PairedAVX2"),
 "quant-scan": ("micro-encoder.txt", "BenchmarkQuantScanFour4x4", "/Fusedfalse", "/Fusedtrue"),
 "cavlc": ("micro-encoder.txt", "BenchmarkCavlcBitWriter", "/prior=true", "/prior=false"),
 "framing": ("micro-framing.txt", "BenchmarkGraphicsFrameStream", "/prior=true", "/prior=false"),
}
for group,(source,prefix,old,new) in families.items():
 text=(base/source).read_text()
 headers=[line for line in text.splitlines() if line.startswith(("goos:","goarch:","pkg:","cpu:"))]
 for label,variant in [("before",old),("after",new)]:
  lines=[line.replace(variant,"") for line in text.splitlines() if line.startswith(prefix) and variant in line and re.search(r"\d+\s+ns/op",line)]
  (base/(group+"-component-"+label+".txt")).write_text("\n".join(headers+lines)+"\n")
 result=subprocess.run(["/home/define42/go/bin/benchstat",str(base/(group+"-component-before.txt")),str(base/(group+"-component-after.txt"))],text=True,capture_output=True,check=True)
 (base/(group+"-component-benchstat.txt")).write_text(result.stdout)
 print(group+"\n"+result.stdout.split("B/op")[0][:5000],flush=True)
