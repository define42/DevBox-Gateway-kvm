from pathlib import Path
import json
import math
import re

root = Path('/home/define42/git/DevBox-Gateway-kvm/docs/benchmarks/software-h264-parity-target-2026-10-22')
p = root / 'round11'
c = json.loads((p / 'codec-summary.json').read_text())
f = json.loads((p / 'full-summary.json').read_text())
rows = [line for line in (p / 'codec-benchstat.txt').read_text().splitlines()
        if line.startswith('OpenH264/')][:10]
ps = [float(re.search(r'p=([0-9.]+)', line)[1]) for line in rows]
assert len(c) == len(ps) == 10
ct = '\n'.join('| ' + x['name'] + ' | ' + ' | '.join(
    f"{x['medians'][k]['ns/op']/1e6:.3f}" for k in ['before', 'after', 'native']) +
    f" | {x['time_change_percent']:.2f}% | {prob:.3f} | {x['native_ratio']:.3f}× |"
    for x, prob in zip(c, ps))
ft = '\n'.join(f"| {x['case'].removeprefix('FullscreenVideo/').removesuffix('/FullDamage-4')} | "
               f"{x['before']['ns/op']/1e6:.3f} | {x['after']['ns/op']/1e6:.3f} | "
               f"{x['wall_change_percent']:.2f}% | {x['p']:.3f} |" for x in f)
ratios = {s: [x['native_ratio'] for x in c if '1920x1080' in x['name'] and f'{s}Slices' in x['name']]
          for s in [1, 4]}
wins = sum(prob <= .05 and x['time_change_percent'] < 0 for x, prob in zip(c, ps))
regressions = sum(prob <= .05 and x['time_change_percent'] > 0 for x, prob in zip(c, ps))
geo = (math.exp(sum(math.log(x['medians']['after']['ns/op'] / x['medians']['before']['ns/op'])
                       for x in c) / len(c)) - 1) * 100
noisy4 = any(any(float(v) > 20 for v in re.findall(r'±\s*([0-9.]+)%', row))
             for row in rows if '4Slices' in row)
four_note = ('Four-slice timing remains highly variable in at least one case; raw median ratios below do not establish reliable native parity.'
             if noisy4 else f'Four-slice median ratios are {min(ratios[4]):.2f}–{max(ratios[4]):.2f}× native.')
full_wins = sum(x['p'] <= .05 and x['wall_change_percent'] < 0 for x in f)
full_losses = sum(x['p'] <= .05 and x['wall_change_percent'] > 0 for x in f)
(p / 'README.md').write_text(f'''# Software H.264 performance parity: round eleven

**Native speed parity remains unmet.** Matched 1080p one-slice Go median times are **{min(ratios[1]):.2f}–{max(ratios[1]):.2f}× native**. {four_note} All checked streams remain byte-identical. CPU consumption is reported alongside wall time; four-slice wall time alone does not establish equal efficiency.

Two changes join the thirty-one retained groups in [round ten](../round10/README.md):

- The three private four-candidate SAD kernels now receive tracked pointers after the existing checks, replacing unused slice headers. Their argument area shrinks from 80 to 48 bytes, with arithmetic, source reads and deferred output stores unchanged. Twelve of fourteen checked-kernel cases improve 4.42–10.12%; two are inconclusive. Twenty-one of 42 complete diamond-search cases improve 1.75–7.27%, with 21 inconclusive and no significant regressions. Complete-caller geomean changes by −3.13%; all cases allocate zero bytes. [Statistics](sad-four-abi-review/benchstat.txt), [contracts and scope](sad-four-abi-next-README.txt).
- Analysis dispatch now selects the missing SSE2 macroblock motion-vector fill, matching native's selected operation. A checked callback broadcasts the copied 32-bit vector and writes exactly 64 bytes. Short slices use the original scalar loop, preserving its partial writes and index panic. All eight dispatched fill cases improve 55.12–63.45%; all eight scalar controls are inconclusive. Of eight complete skip cases that execute the fill, five improve 4.43–7.04% and three are inconclusive (geomean −4.85%). Of six no-fill controls, two also improve 3.22–3.47% and four are inconclusive; these control wins cannot be attributed to the new fill. No case significantly regresses, and all allocate zero bytes. [Statistics](update-mv-review/benchstat.txt), [caller classification](update-mv-simd-next-README.txt).

Component results use six alternating pairs, CPU 2 and 200 ms per case against frozen round ten. They are not additive frame gains. Native's pointer-based calls motivate the SAD argument reduction; native `md.cpp:487` and `mb_copy.asm:359–373` establish the missing SSE2 motion-vector dispatch/fill. The latter was only about 1% of the earlier PDF CPU profile, so its component speedup has a limited frame-level ceiling.

## Complete codec comparison

**Before is frozen round ten.** Six rotating previous/Go/native runs use 16 warmup and 80 measured frames. One slice uses CPU 2; four slices use CPUs 0–3. Intel i7-7700HQ, Go 1.27.3, CGO disabled, GOMAXPROCS=4; other workstation processes remain running. No host performance settings or other processes were changed.

| Case | Round ten ms | Current ms | Native ms | Change vs ten | p-value | Current/native |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
{ct}

There are {wins} significant incremental wall-time reductions, {regressions} significant regressions and {len(c)-wins-regressions} inconclusive cases. The median wall-time geomean changes by {geo:.2f}%. [Complete statistics](codec-benchstat.txt), [native comparison](codec-native-benchstat.txt), [medians and CPU metrics](codec-summary.json). Inconclusive results do not establish equality or a performance gain.

Native is local OpenH264 2.6.0 commit `4c8c5cdc0299407af14751a4c37449ff3d367745`, GCC 13.3 `-O3` with assembly/AVX2. Both use camera real-time, low complexity, QP20 unless QP32 is labeled, one reference/spatial/temporal layer, Baseline/CAVLC, fixed slices, full-range BT.709 and a 60 FPS codec clock. Rate control, deblocking, periodic IDR, dropping, scene/background detection, AQ, denoising, LTR and load balancing are disabled. [Matching provenance](../../software-h264-native-parity-2026-10-22/README.md#inputs-and-matching).

Codec timing includes encoding and an owning payload. RGB conversion, initialization, loading, capture, framing, network/ACK waits and presentation are excluded. Absolute timings across separate rounds are not paired comparisons. Round ten's noisy first pass and bounded confirmation remain archived; no samples from those runs are silently removed or substituted here.

## Full EncodeDamage comparison

Before is original commit `641af9e6216055b6a9d918bdcba4ea7d40020750`; after includes all thirty-three retained groups. Six alternating pairs include RGB conversion, damage preparation, encoding and owning payload construction, with 16 warmup and 60 measured frames. This harness uses FrameRate=30, unlike the codec-only 60 FPS clock. Capture, network/ACK waits and presentation are excluded.

| 1080p case | Original ms | Current ms | Change | p-value |
| --- | ---: | ---: | ---: | ---: |
{ft}

This cumulative comparison has {full_wins} significant reductions, {full_losses} significant regressions and {len(f)-full_wins-full_losses} inconclusive cases. [Full-path statistics](full-benchstat.txt), [exact metrics](full-summary.json). These measurements do not measure the live RDP “Encoding average”.

## Validation and remaining work

All ten streams match original Go and native byte for byte. FFmpeg decoded all 960 frames without errors; independent frame counts agreed. [Hash/decoder evidence](parity.txt). Package/AVC tests, race checks, AVX2-disabled and optional-features-disabled tests, Linux ARM64 cross-builds, gopls on this round’s changed files and cumulative whitespace checks against the original baseline passed. [Commands/results](validation.json). Cross-builds establish build compatibility, not ARM64 runtime performance.

SAD tests preserve exact span and halo checks, output/input aliases, panic type/text/order, negative/zero/extreme strides, complete search state, callback order, strict ties and stack allocation. MV tests cover every int16 component pattern, all byte origins, guard-page boundaries, copied-value aliases, short-slice partial failures, dispatch reset and complete skip-state/callback parity. Caller visit tests confirm the eight fill and six no-fill benchmark cases in both warmup and repeated calls. Production checks and encoder settings remain intact.

The CAVLC trailing-one experiment remains unmerged: only two of 67 cases improve significantly, with one unchanged known-zero control regression and 64 inconclusive results. [Held evidence](../round10/cavlc-trailing-review/decision.txt). Fresh [Road](round11-road1-cum.txt) and [PDF](round11-pdf1-cum.txt) profiles describe the final binary; separate diagnostic streams preserve all 616 encoded frames. [Profile scope](round11-profile-scope.txt). The remaining gap is still distributed across motion estimation/refinement, entropy coding, intra prediction and macroblock work.

The [cumulative patch](encoder-change.patch) applies to the original commit. The [before-source fingerprint](before-sources.sha256) identifies the original production baseline plus five measurement-only benchmark files; all 427 original tracked source files are unchanged ([baseline audit](round11-baseline-audit.json)); the codec-before fingerprint is [round ten](../round10/round10-sources.sha256). The [current fingerprint](round11-sources.sha256), [combined runner](measure_round11.py), raw samples and validation logs fix this milestone. Restore `compare_test.go.txt` and `fullscreen_test.go.txt` to their `.go` names before rebuilding the runner overlays. A final trailing-blank-line cleanup was rebuilt and verified to have byte-identical executable and initialized data sections to the timed binary; [evidence](round11-whitespace-cleanup.json). The [artifact audit](artifact-audit.json) verifies all source hashes and replays the cumulative patch on the pristine original Git tree. Workspace-specific fixture/native paths need adaptation on another machine.
''')
parent = (root / 'README.md').read_text()
parent = parent.replace('thirty-one optimization groups', 'thirty-three optimization groups')
parent = parent.replace('(round10/README.md) contains current', '(round11/README.md) contains current')
parent = parent.replace('round nine to isolate the two newest changes',
                        'round ten to isolate the two newest changes')
newrow = '| [Round eleven](round11/README.md) | Smaller four-candidate SAD arguments; SSE2 macroblock motion-vector fill. |'
if newrow not in parent:
    needle = '| [Round ten](round10/README.md) | Single-call center filters with compact stack scratch; AVX2 SAD-only VAA. |'
    assert needle in parent
    parent = parent.replace(needle, needle + '\n' + newrow)
(root / 'README.md').write_text(parent)
print(p / 'README.md')
