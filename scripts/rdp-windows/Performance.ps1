Set-StrictMode -Version Latest

function Get-LocalWorkload([string] $Value) {
    if ([string]::IsNullOrEmpty($Value)) { return 'smoke' }
    if ($Value -cnotin @('smoke', 'performance')) { throw 'invalid_local_configuration' }
    return $Value
}

function New-ProcessPerformance {
    return @{
        version = 1; scope = 'parent_process_sampling_window'; processorCount = [Environment]::ProcessorCount
        requestedSampleIntervalMs = 250; cpuPercentMeaning = '100_percent_is_one_logical_processor'
        clock = [Diagnostics.Stopwatch]::StartNew()
        samples = [Collections.Generic.List[object]]::new()
        unavailableSamples = 0
    }
}

function Add-ProcessPerformanceSample($State, [string] $Name, [double] $Seconds,
                                    [double] $CPUSeconds, [long] $ResidentBytes, [long] $PeakResidentBytes) {
    if ($Name -cnotin @('server', 'client') -or $Seconds -lt 0 -or $CPUSeconds -lt 0 -or
        $ResidentBytes -lt 0 -or $PeakResidentBytes -lt $ResidentBytes -or
        [double]::IsNaN($Seconds) -or [double]::IsInfinity($Seconds) -or
        [double]::IsNaN($CPUSeconds) -or [double]::IsInfinity($CPUSeconds)) { throw 'invalid_process_sample' }
    if ($State.samples.Count -ge 2048) { throw 'process_sample_limit' }
    $State.samples.Add(@{
        process = $Name; elapsedSeconds = $Seconds; cpuSeconds = $CPUSeconds
        residentBytes = $ResidentBytes; lifetimePeakResidentBytes = $PeakResidentBytes
    })
}

function Measure-LocalProcess($State, $Process, [string] $Name) {
    if ($null -eq $Process -or $Process.HasExited) { return }
    try {
        $Process.Refresh()
        Add-ProcessPerformanceSample $State $Name $State.clock.Elapsed.TotalSeconds `
            $Process.TotalProcessorTime.TotalSeconds $Process.WorkingSet64 $Process.PeakWorkingSet64
    } catch {
        # A process can exit between HasExited and the operating-system query.
        # Missing samples remain visible; they are never synthesized as zero.
        $State.unavailableSamples++
    }
}

function Get-ProcessPerformanceReport($State) {
    $processes = @{}
    foreach ($name in @('server', 'client')) {
        $samples = @($State.samples | Where-Object { $_.process -ceq $name })
        $summary = @{
            sampleCount = $samples.Count; firstSampleSeconds = $null; lastSampleSeconds = $null
            sampleWindowSeconds = $null; cpuSeconds = $null; averageCpuPercent = $null
            maxSampledResidentBytes = $null; lifetimePeakResidentBytes = $null
        }
        if ($samples.Count -gt 0) {
            $summary.firstSampleSeconds = $samples[0].elapsedSeconds
            $summary.lastSampleSeconds = $samples[-1].elapsedSeconds
            $summary.maxSampledResidentBytes = ($samples | Measure-Object residentBytes -Maximum).Maximum
            $summary.lifetimePeakResidentBytes = ($samples | Measure-Object lifetimePeakResidentBytes -Maximum).Maximum
        }
        if ($samples.Count -ge 2) {
            $duration = $samples[-1].elapsedSeconds - $samples[0].elapsedSeconds
            $cpu = $samples[-1].cpuSeconds - $samples[0].cpuSeconds
            if ($duration -gt 0 -and $cpu -ge 0) {
                $summary.sampleWindowSeconds = $duration
                $summary.cpuSeconds = $cpu
                $summary.averageCpuPercent = 100 * $cpu / $duration
            }
        }
        $processes[$name] = $summary
    }
    return @{
        version = 1; scope = $State.scope; processorCount = $State.processorCount
        requestedSampleIntervalMs = $State.requestedSampleIntervalMs; cpuPercentMeaning = $State.cpuPercentMeaning
        unavailableSamples = $State.unavailableSamples; processes = $processes; samples = @($State.samples.ToArray())
    }
}

function Test-PerformanceNumber($Value, [double] $Minimum = 0) {
    return ($Value -is [int] -or $Value -is [long] -or $Value -is [double] -or $Value -is [decimal]) -and
        -not [double]::IsNaN([double]$Value) -and -not [double]::IsInfinity([double]$Value) -and
        [double]$Value -ge $Minimum
}

function Assert-PerformanceReady($Ready, [string] $Mode) {
    try {
        Assert-LocalReady $Ready $Mode
        foreach ($field in @('width', 'height', 'targetFps')) {
            if ($Ready[$field] -isnot [int] -and $Ready[$field] -isnot [long]) { throw 'invalid_performance_endpoint' }
        }
        if ($Ready.workload -cne 'performance' -or $Ready.width -ne 1920 -or $Ready.height -ne 1080 -or
            $Ready.targetFps -ne 30) { throw 'invalid_performance_endpoint' }
    } catch { throw 'invalid_performance_endpoint' }
}

function Assert-PerformanceEvidence($Server, $Events, $Client, $Processes, [string] $Mode) {
    try {
        $encoder = if ($Mode -ceq 'bitmap') { 'bitmap' } else { 'software' }
        if ($Mode -cnotin @('bitmap', 'h264') -or $Server.version -ne 1 -or
            $Server.mode -cne $Mode -or $Server.workload -cne 'performance' -or $Server.outcome -cne 'completed' -or
            $Server.width -ne 1920 -or $Server.height -ne 1080 -or $Server.targetFps -ne 30 -or
            $Server.actualEncoder -cne $encoder -or -not (Test-PerformanceNumber $Server.framesSent 1) -or
            -not (Test-PerformanceNumber $Server.sessionSeconds 30) -or
            -not (Test-PerformanceNumber $Server.inputTransitions 3) -or
            ($Mode -ceq 'h264' -and $Server.graphicsReady -ne $true)) { throw 'server_invalid' }
        if ($Client.version -ne 1 -or $Client.workload -cne 'performance' -or $Client.outcome -cne 'passed' -or
            $Client.measurementComplete -ne $true -or $Client.sourceWidth -ne 1920 -or $Client.sourceHeight -ne 1080 -or
            $Client.warmupSeconds -ne 5 -or $Client.requestedCaptureIntervalMs -ne 16 -or
            -not (Test-PerformanceNumber $Client.measurementSeconds 30) -or
            -not (Test-PerformanceNumber $Client.validSamples 2) -or
            -not (Test-PerformanceNumber $Client.visibleUpdates 1) -or
            -not (Test-PerformanceNumber $Client.inputResponsesReceived 3) -or
            $Client.inputResponsesReceived -ne $Client.inputProbesSent -or
            $Server.inputTransitions -lt $Client.inputResponsesReceived -or
            $Client.inputLatencyMs.count -ne $Client.inputResponsesReceived -or
            @($Client.inputLatencySamplesMs).Count -ne $Client.inputResponsesReceived) { throw 'client_invalid' }
        foreach ($field in @('sampleCount', 'validSamples', 'invalidSamples', 'visibleUpdates',
                             'unobservedSequenceNumbers', 'stallEpisodes', 'sampledUpdatesPerSecond', 'maxVisibleGapMs')) {
            if (-not (Test-PerformanceNumber $Client[$field])) { throw 'client_metrics_invalid' }
        }
        if (-not (Test-PerformanceNumber $Client.captureWidth 1) -or -not (Test-PerformanceNumber $Client.captureHeight 1) -or
            $Client.sampleCount -ne $Client.validSamples + $Client.invalidSamples -or
            $Client.visibleUpdates -gt $Client.validSamples -or $Client.captureMs.count -ne $Client.sampleCount -or
            $Client.sampleIntervalMs.count -ne $Client.sampleCount - 1) { throw 'client_counts_invalid' }
        foreach ($timing in @($Client.inputLatencyMs, $Client.captureMs, $Client.sampleIntervalMs)) {
            if (-not (Test-PerformanceNumber $timing.count 1)) { throw 'timing_invalid' }
            foreach ($field in @('average', 'p50', 'p95', 'max')) {
                if (-not (Test-PerformanceNumber $timing[$field])) { throw 'timing_invalid' }
            }
            if ($timing.p50 -gt $timing.p95 -or $timing.p95 -gt $timing.max) { throw 'timing_invalid' }
        }
        foreach ($latency in $Client.inputLatencySamplesMs) {
            if (-not (Test-PerformanceNumber $latency)) { throw 'timing_invalid' }
        }
        if ($Events.version -ne 1 -or @($Events.events | Where-Object { $_.outcome -ceq 'failed' }).Count -gt 0) {
            throw 'events_invalid'
        }
        $completed = @($Events.events | Where-Object {
            $_.stage -ceq 'benchmark' -and $_.outcome -ceq 'passed' -and $_.reason -ceq 'performance_measurement_completed'
        })
        if ($completed.Count -ne 1) { throw 'events_invalid' }
        if ($Processes.version -ne 1 -or $Processes.scope -cne 'parent_process_sampling_window') { throw 'processes_invalid' }
        foreach ($name in @('server', 'client')) {
            $process = $Processes.processes[$name]
            if (-not (Test-PerformanceNumber $process.sampleCount 2) -or
                -not (Test-PerformanceNumber $process.sampleWindowSeconds 0.001) -or
                -not (Test-PerformanceNumber $process.averageCpuPercent) -or
                -not (Test-PerformanceNumber $process.maxSampledResidentBytes 1)) { throw 'processes_invalid' }
        }
        $metrics = $Server.performance
        if ($null -eq $metrics -or $metrics.scope -cne 'session_including_warmup' -or
            -not (Test-PerformanceNumber $metrics.durationSeconds 30) -or
            -not (Test-PerformanceNumber $metrics.transportSeconds 30) -or
            -not (Test-PerformanceNumber $metrics.framesPerSecond) -or
            -not (Test-PerformanceNumber $metrics.sentBytes 1) -or
            -not (Test-PerformanceNumber $metrics.receivedBytes 1) -or
            -not (Test-PerformanceNumber $metrics.sentMbps) -or
            -not (Test-PerformanceNumber $metrics.receivedMbps)) { throw 'server_performance_invalid' }
        $timings = @($metrics.frameMs, $metrics.writeMs)
        if ($Mode -ceq 'h264') { $timings += @($metrics.encodeMs, $metrics.convertMs) }
        else { $timings += @($metrics.bitmapPackingMs) }
        foreach ($timing in $timings) {
            if ($null -eq $timing -or -not (Test-PerformanceNumber $timing.count 1)) { throw 'server_timing_invalid' }
            foreach ($field in @('average', 'p50', 'p95', 'maximum')) {
                if (-not (Test-PerformanceNumber $timing[$field])) { throw 'server_timing_invalid' }
            }
            if ($timing.p50 -gt $timing.p95 -or $timing.p95 -gt $timing.maximum) { throw 'server_timing_invalid' }
        }
    } catch { throw 'benchmark_evidence_failed' }
}

function Read-OptionalPerformanceJSON([string] $Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    try { return Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json -AsHashtable }
    catch { return $null }
}

function Write-PerformanceArtifacts([string] $OutputPath, $State, [string] $Mode,
                                    [string] $Outcome, [string] $Reason) {
    $processes = Get-ProcessPerformanceReport $State
    $processes | ConvertTo-Json -Depth 15 | Set-Content -LiteralPath (Join-Path $OutputPath 'process-performance.json') -Encoding utf8 -ErrorAction Stop
    $server = Read-OptionalPerformanceJSON (Join-Path $OutputPath 'server.json')
    $client = Read-OptionalPerformanceJSON (Join-Path $OutputPath 'client-performance.json')
    $report = @{
        version = 1; testKind = 'windows-local-rdp-performance'; outcome = $Outcome; reason = $Reason
        mode = $Mode; harnessCommit = $env:GITHUB_SHA; generatedAt = [datetime]::UtcNow.ToString('o')
        performanceThresholdsApplied = $false; testsKVM = $false; testsGatewayAuthentication = $false
        server = $server; client = $client; processes = $processes
        notes = @(
            'Baseline measurements only: no throughput or latency regression thresholds are applied.'
            'Client measurement begins after its five-second visual warmup. Server statistics include session warmup; process windows are reported independently.'
            'Visible updates are sampled screen progress, not exact presentation FPS. Unobserved sequence numbers are not proven dropped frames.'
            'Input latency includes frame pacing and screenshot observation. Inspect capture and sample intervals when comparing runs.'
            'CPU percent is per process: 100 percent is one logical processor. Memory is host resident memory, not guest RAM.'
            'Both endpoints share a hosted Windows runner over loopback. This does not benchmark KVM, the gateway, or a real network.'
        )
    }
    $summaryCompleted = $false
    try {
        if (-not [string]::IsNullOrEmpty($env:GITHUB_STEP_SUMMARY)) {
            $lines = @(('### Windows RDP performance baseline (' + $Mode + ')'), '',
                ('Outcome: **' + $Outcome + '**. No performance thresholds applied.'), '',
                '| Measurement | Value |', '| --- | --- |')
            if ($Outcome -ceq 'passed' -and $null -ne $client -and $client.ContainsKey('measurementComplete') -and $client.measurementComplete) {
                $lines += @(
                    ('| Source / capture resolution | 1920×1080 / {0}×{1} |' -f $client.captureWidth, $client.captureHeight),
                    ('| Client measurement window | {0:N2} s |' -f $client.measurementSeconds),
                    ('| Sampled visible updates | {0:N2}/s |' -f $client.sampledUpdatesPerSecond),
                    ('| Longest visible progress gap | {0:N2} ms |' -f $client.maxVisibleGapMs),
                    ('| Input responses | {0} |' -f $client.inputResponsesReceived),
                    ('| Input latency p50 / p95 | {0:N2} / {1:N2} ms |' -f $client.inputLatencyMs.p50, $client.inputLatencyMs.p95))
            }
            if ($Outcome -ceq 'passed' -and $null -ne $server -and $server.ContainsKey('performance') -and $null -ne $server.performance) {
                $metrics = $server.performance
                $lines += @(
                    ('| Server sent frames / second (session) | {0:N2} |' -f $metrics.framesPerSecond),
                    ('| Transport sent / received (connection) | {0:N2} / {1:N2} Mbps |' -f $metrics.sentMbps, $metrics.receivedMbps))
                if ($null -ne $metrics.encodeMs) {
                    $lines += ('| H.264 encoding p50 / p95 | {0:N2} / {1:N2} ms |' -f $metrics.encodeMs.p50, $metrics.encodeMs.p95)
                }
            }
            foreach ($name in @('server', 'client')) {
                $process = $processes.processes[$name]
                if ($null -ne $process.averageCpuPercent) {
                    $lines += ('| {0} CPU / max sampled RSS | {1:N2}% / {2:N2} MiB |' -f
                        $name, $process.averageCpuPercent, ($process.maxSampledResidentBytes / 1MB))
                }
            }
            $lines += @('', 'See `benchmark.json` for timings, traffic, process samples, measurement windows, and limitations.')
            $lines -join "`n" | Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY -Encoding utf8 -ErrorAction Stop
        }
        $summaryCompleted = $true
    } finally {
        # Publishing a summary must not leave a previously passed aggregate
        # behind when the parent reports the benchmark as failed.
        if (-not $summaryCompleted) {
            $report.outcome = 'failed'
            if ([string]::IsNullOrEmpty($report.reason)) { $report.reason = 'performance_report_failed' }
        }
        $report | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath (Join-Path $OutputPath 'benchmark.json') -Encoding utf8 -ErrorAction Stop
    }
}
