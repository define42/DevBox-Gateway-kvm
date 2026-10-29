. (Join-Path $PSScriptRoot 'Performance.ps1')

Assert-True ((Get-LocalWorkload $null) -ceq 'smoke') 'missing workload preserves compatibility defaults'
Assert-True ((Get-LocalWorkload 'smoke') -ceq 'smoke') 'explicit compatibility workload'
Assert-True ((Get-LocalWorkload 'performance') -ceq 'performance') 'explicit performance workload'
foreach ($invalid in @('PERFORMANCE', 'other', ' performance')) {
    Assert-Rejected { Get-LocalWorkload $invalid | Out-Null } 'invalid workload cannot silently fall back'
}

$performanceReady = @{ address = '127.0.0.1:54321'; port = 54321; mode = 'bitmap';
    workload = 'performance'; width = 1920; height = 1080; targetFps = 30 }
Assert-PerformanceReady $performanceReady 'bitmap'
foreach ($change in @(@{ Key = 'width'; Value = 1024 }, @{ Key = 'height'; Value = 768 },
                     @{ Key = 'targetFps'; Value = 2 }, @{ Key = 'workload'; Value = 'smoke' },
                     @{ Key = 'width'; Value = '1920' })) {
    $badReady = $performanceReady.Clone()
    $badReady[$change.Key] = $change.Value
    Assert-Rejected { Assert-PerformanceReady $badReady 'bitmap' } 'smoke fixture cannot claim performance evidence'
}

$processState = New-ProcessPerformance
$emptyProcesses = Get-ProcessPerformanceReport $processState
Assert-True ($null -eq $emptyProcesses.processes.server.averageCpuPercent) 'missing CPU remains unavailable'
Add-ProcessPerformanceSample $processState 'server' 1 4 1MB 2MB
Add-ProcessPerformanceSample $processState 'server' 3 5 2MB 3MB
Add-ProcessPerformanceSample $processState 'client' 2 8 2MB 3MB
Add-ProcessPerformanceSample $processState 'client' 4 8.5 3MB 4MB
$processReport = Get-ProcessPerformanceReport $processState
Assert-True ($processReport.processes.server.averageCpuPercent -eq 50) 'CPU derives from CPU-time delta over elapsed window'
Assert-True ($processReport.processes.client.averageCpuPercent -eq 25) 'client CPU measured independently'
Assert-True ($processReport.processes.server.maxSampledResidentBytes -eq 2MB) 'sampled RSS maximum'
Assert-True ($processReport.processes.server.lifetimePeakResidentBytes -eq 3MB) 'lifetime RSS peak is distinct'
Assert-True ($processReport.processes.client.firstSampleSeconds -eq 2 -and
             $processReport.processes.client.lastSampleSeconds -eq 4) 'process-specific window retained'
Assert-Rejected { Add-ProcessPerformanceSample $processState 'server' ([double]::NaN) 0 0 0 } 'invalid sample rejected'
Assert-Rejected { Add-ProcessPerformanceSample $processState 'unknown' 0 0 0 0 } 'unknown process rejected'

$clientTiming = @{ count = 3; average = 40.0; p50 = 40.0; p95 = 60.0; max = 60.0 }
$serverTiming = @{ count = 60; samples = 60; truncated = $false; average = 3.0; p50 = 2.0; p95 = 5.0; maximum = 6.0 }
$benchmarkServer = @{
    version = 1; mode = 'bitmap'; workload = 'performance'; outcome = 'completed'; actualEncoder = 'bitmap'
    width = 1920; height = 1080; targetFps = 30; framesSent = 60; inputTransitions = 3
    sessionSeconds = 36.0; graphicsReady = $false
    performance = @{
        scope = 'session_including_warmup'; durationSeconds = 36.0; transportSeconds = 37.0
        framesPerSecond = 1.67; sentBytes = 1000000; receivedBytes = 1000; sentMbps = 0.2; receivedMbps = 0.001
        frameMs = $serverTiming; writeMs = $serverTiming; bitmapPackingMs = $serverTiming
        encodeMs = $null; convertMs = $null
    }
}
$benchmarkClient = @{
    version = 1; workload = 'performance'; outcome = 'passed'; reason = 'performance_measurement_completed'
    measurementComplete = $true; measurementSeconds = 30.1; warmupSeconds = 5.0
    requestedCaptureIntervalMs = 16
    sourceWidth = 1920; sourceHeight = 1080; captureWidth = 1024; captureHeight = 576
    validSamples = 100; visibleUpdates = 59; sampleCount = 110; invalidSamples = 10
    sampledUpdatesPerSecond = 1.96; maxVisibleGapMs = 500.0; inputProbesSent = 3; inputResponsesReceived = 3
    unobservedSequenceNumbers = 0; stallEpisodes = 2
    inputLatencyMs = $clientTiming; captureMs = $clientTiming.Clone(); sampleIntervalMs = $clientTiming.Clone()
    inputLatencySamplesMs = @(20.0, 40.0, 60.0)
}
$benchmarkClient.captureMs.count = 110
$benchmarkClient.sampleIntervalMs.count = 109
$benchmarkEvents = @{ version = 1; events = @(
    @{ stage = 'benchmark'; outcome = 'passed'; reason = 'performance_measurement_completed' }
) }
# Slow but complete work is a valid baseline, not a throughput failure.
Assert-PerformanceEvidence $benchmarkServer $benchmarkEvents $benchmarkClient $processReport 'bitmap'
foreach ($change in @(@{ Key = 'measurementComplete'; Value = $false }, @{ Key = 'outcome'; Value = 'failed' },
                     @{ Key = 'measurementSeconds'; Value = 29.9 }, @{ Key = 'inputResponsesReceived'; Value = 0 },
                     @{ Key = 'inputProbesSent'; Value = 4 }, @{ Key = 'visibleUpdates'; Value = 0 },
                     @{ Key = 'sampledUpdatesPerSecond'; Value = [double]::NaN }, @{ Key = 'sampleCount'; Value = 109 },
                     @{ Key = 'sourceWidth'; Value = 1024 }, @{ Key = 'inputLatencySamplesMs'; Value = @(20.0) })) {
    $badClient = $benchmarkClient.Clone()
    $badClient[$change.Key] = $change.Value
    Assert-Rejected { Assert-PerformanceEvidence $benchmarkServer $benchmarkEvents $badClient $processReport 'bitmap' } 'incomplete benchmark rejected'
}
$badClient = $benchmarkClient.Clone()
$badClient.inputLatencyMs = @{ count = 3; average = [double]::NaN; p50 = 40; p95 = 60; max = 60 }
Assert-Rejected { Assert-PerformanceEvidence $benchmarkServer $benchmarkEvents $badClient $processReport 'bitmap' } 'nonfinite timing rejected'
Assert-Rejected { Assert-PerformanceEvidence $benchmarkServer $benchmarkEvents $benchmarkClient $emptyProcesses 'bitmap' } 'missing process measurements rejected'
$badServer = $benchmarkServer.Clone()
$badServer.Remove('performance')
Assert-Rejected { Assert-PerformanceEvidence $badServer $benchmarkEvents $benchmarkClient $processReport 'bitmap' } 'missing server metrics rejected'
$badEvents = @{ version = 1; events = @($benchmarkEvents.events[0], @{ stage = 'connection'; outcome = 'failed' }) }
Assert-Rejected { Assert-PerformanceEvidence $benchmarkServer $badEvents $benchmarkClient $processReport 'bitmap' } 'disconnect overrides complete benchmark'
Assert-Rejected { Assert-PerformanceEvidence $benchmarkServer @{version = 1; events = @()} $benchmarkClient $processReport 'bitmap' } 'missing completion event rejected'
$h264Server = $benchmarkServer.Clone()
$h264Server.mode = 'h264'
$h264Server.actualEncoder = 'software'
$h264Server.graphicsReady = $true
$h264Server.performance = $benchmarkServer.performance.Clone()
$h264Server.performance.encodeMs = $serverTiming
$h264Server.performance.convertMs = $serverTiming
Assert-PerformanceEvidence $h264Server $benchmarkEvents $benchmarkClient $processReport 'h264'

$performanceDirectory = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
$previousSummary = $env:GITHUB_STEP_SUMMARY
try {
    [void][IO.Directory]::CreateDirectory($performanceDirectory)
    $env:GITHUB_STEP_SUMMARY = Join-Path $performanceDirectory 'summary.md'
    $benchmarkServer | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $performanceDirectory 'server.json')
    $benchmarkClient | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $performanceDirectory 'client-performance.json')
    Write-PerformanceArtifacts $performanceDirectory $processState 'bitmap' 'passed' $null
    $saved = Get-Content -Raw (Join-Path $performanceDirectory 'benchmark.json') | ConvertFrom-Json -AsHashtable
    Assert-True ($saved.outcome -ceq 'passed' -and -not $saved.performanceThresholdsApplied -and
                 -not $saved.testsKVM -and -not $saved.testsGatewayAuthentication) 'baseline scope preserved'
    Assert-True ($saved.server.performance.scope -ceq 'session_including_warmup' -and
                 $saved.client.measurementSeconds -eq 30.1 -and
                 $saved.processes.processes.server.sampleWindowSeconds -eq 2) 'different measurement windows preserved'
    $summary = Get-Content -Raw $env:GITHUB_STEP_SUMMARY
    Assert-True ($summary.Contains('Input latency') -and $summary.Contains('CPU') -and $summary.Contains('Mbps')) 'workflow summary contains real measurements'
    Write-PerformanceArtifacts $performanceDirectory $processState 'bitmap' 'failed' 'client_failed'
    $saved = Get-Content -Raw (Join-Path $performanceDirectory 'benchmark.json') | ConvertFrom-Json -AsHashtable
    Assert-True ($saved.outcome -ceq 'failed' -and $saved.reason -ceq 'client_failed') 'failure cannot become a passed baseline'
    $env:GITHUB_STEP_SUMMARY = Join-Path $performanceDirectory 'summary-directory'
    [void][IO.Directory]::CreateDirectory($env:GITHUB_STEP_SUMMARY)
    Assert-Rejected { Write-PerformanceArtifacts $performanceDirectory $processState 'bitmap' 'passed' $null } 'summary publication failure reported'
    $saved = Get-Content -Raw (Join-Path $performanceDirectory 'benchmark.json') | ConvertFrom-Json -AsHashtable
    Assert-True ($saved.outcome -ceq 'failed' -and $saved.reason -ceq 'performance_report_failed') 'summary failure cannot retain a passed aggregate'
} finally {
    $env:GITHUB_STEP_SUMMARY = $previousSummary
    foreach ($name in @('server.json', 'client-performance.json', 'process-performance.json', 'benchmark.json', 'summary.md')) {
        [IO.File]::Delete((Join-Path $performanceDirectory $name))
    }
    $summaryDirectory = Join-Path $performanceDirectory 'summary-directory'
    if ([IO.Directory]::Exists($summaryDirectory)) { [IO.Directory]::Delete($summaryDirectory) }
    [IO.Directory]::Delete($performanceDirectory)
}
