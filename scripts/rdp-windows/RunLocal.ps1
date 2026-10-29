$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Gateway.ps1') # Reuse sanitized Windows event export only.
. (Join-Path $PSScriptRoot 'Local.ps1')
. (Join-Path $PSScriptRoot 'CertificateTrust.ps1')
. (Join-Path $PSScriptRoot 'Performance.ps1')

$stage = 'configuration'
$reason = $null
$outcome = 'failed'
$started = [datetime]::UtcNow
$server = $null
$client = $null
$trustHelperStopped = $true
$cleanupErrors = [Collections.Generic.List[string]]::new()
$statePath = $null
$workload = 'smoke'
$performance = $null
$outputPath = $env:RDP_TEST_OUTPUT
if ([string]::IsNullOrWhiteSpace($outputPath)) { throw 'RDP_TEST_OUTPUT is required.' }
[void][IO.Directory]::CreateDirectory($outputPath)
# Reject stale output before entering artifact-producing cleanup, so even a
# failed rerun cannot overwrite evidence from a previous run.
if (@(Get-ChildItem -LiteralPath $outputPath -Force).Count -ne 0) { throw 'output_directory_not_empty' }

try {
    Write-LocalProgress $outputPath $stage
    $workload = Get-LocalWorkload $env:RDP_TEST_WORKLOAD
    if ($workload -ceq 'performance') { $performance = New-ProcessPerformance }
    if (-not $IsWindows -or $env:RDP_TEST_MODE -cnotin @('bitmap', 'h264') -or
        [string]::IsNullOrEmpty($env:RUNNER_TEMP)) { throw 'invalid_local_configuration' }
    Assert-FixtureTrustEnvironment
    $serverExe = Join-Path $env:RUNNER_TEMP 'rdp-server.exe'
    $clientExe = Join-Path $env:RUNNER_TEMP 'rdp-client.exe'
    if (-not (Test-Path $serverExe) -or -not (Test-Path $clientExe)) { throw 'executables_not_built' }
    $statePath = Join-Path $env:RUNNER_TEMP ('rdp-local-' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($statePath)

    $stage = 'server_start'
    Write-LocalProgress $outputPath $stage
    $launch = [Diagnostics.ProcessStartInfo]::new($serverExe)
    $launch.UseShellExecute = $false
    foreach ($argument in @('-mode', $env:RDP_TEST_MODE, '-state-dir', $statePath, '-output', $outputPath, '-timeout', '150s')) {
        [void]$launch.ArgumentList.Add($argument)
    }
    if ($workload -ceq 'performance') {
        [void]$launch.ArgumentList.Add('-workload')
        [void]$launch.ArgumentList.Add('performance')
    }
    $server = [Diagnostics.Process]::Start($launch)
    if ($null -ne $performance) { Measure-LocalProcess $performance $server 'server' }
    $readyPath = Join-Path $statePath 'ready.json'
    $ready = Wait-LocalReady $readyPath $server
    Assert-LocalReady $ready $env:RDP_TEST_MODE
    if ($workload -ceq 'performance') { Assert-PerformanceReady $ready $env:RDP_TEST_MODE }

    $stage = 'temporary_certificate_trust'
    Write-LocalProgress $outputPath $stage
    Invoke-LocalCertificateTrust 'Install' $statePath
    $profilePath = Join-Path $statePath 'desktop.rdp'
    [IO.File]::WriteAllText($profilePath, (New-LocalProfile $ready))

    $stage = 'microsoft_client'
    Write-LocalProgress $outputPath $stage
    $launch = [Diagnostics.ProcessStartInfo]::new($clientExe)
    $launch.UseShellExecute = $false
    $launch.Environment['RDP_SMOKE_PROFILE'] = $profilePath
    $launch.Environment['RDP_SMOKE_PASSWORD'] = 'unused-local-fixture-password'
    $launch.Environment['RDP_SMOKE_OUTPUT'] = $outputPath
    $launch.Environment['RDP_SMOKE_DURATION_SECONDS'] = '30'
    $launch.Environment['RDP_SMOKE_LOCAL'] = '1'
    $launch.Environment['RDP_SMOKE_WORKLOAD'] = $workload
    $client = [Diagnostics.Process]::Start($launch)
    if ($null -ne $performance) {
        Measure-LocalProcess $performance $server 'server'
        Measure-LocalProcess $performance $client 'client'
    }
    $deadline = [datetime]::UtcNow.AddSeconds(130)
    while (-not $client.HasExited) {
        if ([datetime]::UtcNow -gt $deadline) { throw 'client_timeout' }
        if ($server.HasExited -and $server.ExitCode -ne 0) { throw 'server_failed' }
        Start-Sleep -Milliseconds 250
        $client.Refresh()
        $server.Refresh()
        if ($null -ne $performance) {
            Measure-LocalProcess $performance $server 'server'
            Measure-LocalProcess $performance $client 'client'
        }
    }
    $client.WaitForExit()
    if ($client.ExitCode -ne 0) { throw 'client_failed' }
    $stage = 'server_completion'
    Write-LocalProgress $outputPath $stage
    if (-not $server.WaitForExit(10000)) { throw 'server_completion_timeout' }
    if ($server.ExitCode -ne 0) { throw 'server_failed' }

    $stage = 'evidence_verification'
    Write-LocalProgress $outputPath $stage
    $serverReport = Get-Content -Raw -LiteralPath (Join-Path $outputPath 'server.json') | ConvertFrom-Json -AsHashtable
    $clientReport = Get-Content -Raw -LiteralPath (Join-Path $outputPath 'client-events.json') | ConvertFrom-Json -AsHashtable
    if ($workload -ceq 'performance') {
        $clientPerformance = Get-Content -Raw -LiteralPath (Join-Path $outputPath 'client-performance.json') | ConvertFrom-Json -AsHashtable
        Assert-PerformanceEvidence $serverReport $clientReport $clientPerformance (Get-ProcessPerformanceReport $performance) $env:RDP_TEST_MODE
    } else {
        Assert-LocalEvidence $serverReport $clientReport $env:RDP_TEST_MODE
    }
    $outcome = 'passed'
} catch {
    $reason = 'operation_failed'
    $allowed = @('invalid_local_configuration', 'executables_not_built',
        'server_start_failed', 'server_ready_timeout', 'invalid_local_endpoint', 'invalid_fixture_certificate',
        'fixture_certificate_already_present', 'fixture_trust_not_authorized', 'fixture_trust_requires_administrator',
        'fixture_certificate_timeout', 'fixture_certificate_install_failed', 'fixture_certificate_operation_failed',
        'fixture_certificate_child_cleanup_failed', 'client_timeout', 'client_failed', 'server_failed',
        'server_completion_timeout', 'server_evidence_failed', 'client_evidence_failed', 'client_duration_failed',
        'invalid_performance_endpoint', 'benchmark_evidence_failed')
    if ($_.Exception.Message -cin $allowed) { $reason = $_.Exception.Message }
    if ($reason -eq 'fixture_certificate_child_cleanup_failed') { $trustHelperStopped = $false }
    Write-Host ('Windows-only RDP test failed at stage: ' + $stage + ' (' + $reason + ')')
} finally {
    Write-LocalCleanupProgress $outputPath 'process_cleanup' $cleanupErrors
    foreach ($process in @($client, $server)) {
        try { Stop-LocalProcess $process }
        catch { $cleanupErrors.Add('process_cleanup_failed') }
    }
    if ($null -ne $statePath) {
        Write-LocalCleanupProgress $outputPath 'certificate_cleanup' $cleanupErrors
        $trustCleaned = Clear-LocalCertificateTrust $statePath $trustHelperStopped $cleanupErrors
        # Delete only named fixture outputs in the unique directory we created.
        # Refuse to recursively delete arbitrary contents or a caller's directory.
        # Preserve ownership evidence if certificate removal failed.
        try {
            Write-LocalCleanupProgress $outputPath 'state_cleanup' $cleanupErrors
            $names = @('ready.json', 'desktop.rdp')
            if ($trustCleaned) { $names += @('server.cer', 'trust-receipt.json', 'trust-Install.json', 'trust-Remove.json') }
            foreach ($name in $names) {
                $path = Join-Path $statePath $name
                if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
            }
            if ($trustCleaned) { [IO.Directory]::Delete($statePath) }
        } catch { $cleanupErrors.Add('state_cleanup_failed') }
    }
    Write-LocalCleanupProgress $outputPath 'event_export' $cleanupErrors
    try { Export-TestEvents $started (Join-Path $outputPath 'windows-events.json') }
    catch { $cleanupErrors.Add('event_export_failed') }
    if ($cleanupErrors.Count -gt 0) {
        $outcome = 'failed'
        if ($null -eq $reason) { $reason = $cleanupErrors[0] }
    }
    if ($null -ne $performance) {
        try { Write-PerformanceArtifacts $outputPath $performance $env:RDP_TEST_MODE $outcome $reason }
        catch {
            $outcome = 'failed'
            if ($null -eq $reason) { $reason = 'performance_report_failed' }
        }
    }
    @{
        schema = 1; testKind = 'windows-local-rdp'; outcome = $outcome; stage = $stage; reason = $reason
        mode = $env:RDP_TEST_MODE; workload = $workload; startedAt = $started.ToString('o'); finishedAt = [datetime]::UtcNow.ToString('o')
        harnessCommit = $env:GITHUB_SHA; testsKVM = $false; testsGatewayAuthentication = $false
        cleanupErrors = @($cleanupErrors.ToArray())
    } | ConvertTo-Json | Set-Content (Join-Path $outputPath 'result.json') -Encoding utf8
}
if ($outcome -ne 'passed') { exit 1 }
if ($workload -ceq 'performance') {
    Write-Host 'Windows-only RDP performance baseline collected; no throughput or latency thresholds applied.'
} else {
    Write-Host 'Windows-only RDP rendering, F8 input, 30-second session, and actual encoder checks passed.'
}
