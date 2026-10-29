$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Gateway.ps1') # Reuse sanitized Windows event export only.
. (Join-Path $PSScriptRoot 'Local.ps1')

$stage = 'configuration'
$reason = $null
$outcome = 'failed'
$started = [datetime]::UtcNow
$server = $null
$client = $null
$certificate = $null
$store = $null
$trustAdded = $false
$statePath = $null
$outputPath = $env:RDP_TEST_OUTPUT
if ([string]::IsNullOrWhiteSpace($outputPath)) { throw 'RDP_TEST_OUTPUT is required.' }
[void][IO.Directory]::CreateDirectory($outputPath)
# Reject stale output before entering artifact-producing cleanup, so even a
# failed rerun cannot overwrite evidence from a previous run.
if (@(Get-ChildItem -LiteralPath $outputPath -Force).Count -ne 0) { throw 'output_directory_not_empty' }

try {
    if (-not $IsWindows -or $env:RDP_TEST_MODE -cnotin @('bitmap', 'h264') -or
        [string]::IsNullOrEmpty($env:RUNNER_TEMP)) { throw 'invalid_local_configuration' }
    $serverExe = Join-Path $env:RUNNER_TEMP 'rdp-server.exe'
    $clientExe = Join-Path $env:RUNNER_TEMP 'rdp-client.exe'
    if (-not (Test-Path $serverExe) -or -not (Test-Path $clientExe)) { throw 'executables_not_built' }
    $statePath = Join-Path $env:RUNNER_TEMP ('rdp-local-' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($statePath)

    $stage = 'server_start'
    $launch = [Diagnostics.ProcessStartInfo]::new($serverExe)
    $launch.UseShellExecute = $false
    foreach ($argument in @('-mode', $env:RDP_TEST_MODE, '-state-dir', $statePath, '-output', $outputPath, '-timeout', '150s')) {
        [void]$launch.ArgumentList.Add($argument)
    }
    $server = [Diagnostics.Process]::Start($launch)
    $readyPath = Join-Path $statePath 'ready.json'
    $deadline = [datetime]::UtcNow.AddSeconds(20)
    while (-not (Test-Path -LiteralPath $readyPath)) {
        if ($server.HasExited) { throw 'server_start_failed' }
        if ([datetime]::UtcNow -gt $deadline) { throw 'server_ready_timeout' }
        Start-Sleep -Milliseconds 100
        $server.Refresh()
    }
    $ready = Get-Content -Raw -LiteralPath $readyPath | ConvertFrom-Json -AsHashtable
    Assert-LocalReady $ready $env:RDP_TEST_MODE

    $stage = 'temporary_certificate_trust'
    $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new((Join-Path $statePath 'server.cer'))
    if ($certificate.HasPrivateKey -or $certificate.Subject -cne $certificate.Issuer -or
        $certificate.NotAfter.ToUniversalTime() -le [datetime]::UtcNow -or
        $certificate.NotBefore.ToUniversalTime() -gt [datetime]::UtcNow) { throw 'invalid_fixture_certificate' }
    $store = [Security.Cryptography.X509Certificates.X509Store]::new('Root', 'CurrentUser')
    $store.Open([Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
    if ($store.Certificates.Find('FindByThumbprint', $certificate.Thumbprint, $false).Count -ne 0) {
        throw 'fixture_certificate_already_present'
    }
    # Only this freshly generated public localhost certificate is trusted. TLS
    # chain and hostname checking in Microsoft's client remain enabled.
    $store.Add($certificate)
    $trustAdded = $true
    $profilePath = Join-Path $statePath 'desktop.rdp'
    [IO.File]::WriteAllText($profilePath, (New-LocalProfile $ready))

    $stage = 'microsoft_client'
    $launch = [Diagnostics.ProcessStartInfo]::new($clientExe)
    $launch.UseShellExecute = $false
    $launch.Environment['RDP_SMOKE_PROFILE'] = $profilePath
    $launch.Environment['RDP_SMOKE_PASSWORD'] = 'unused-local-fixture-password'
    $launch.Environment['RDP_SMOKE_OUTPUT'] = $outputPath
    $launch.Environment['RDP_SMOKE_DURATION_SECONDS'] = '30'
    $launch.Environment['RDP_SMOKE_LOCAL'] = '1'
    $client = [Diagnostics.Process]::Start($launch)
    $deadline = [datetime]::UtcNow.AddSeconds(130)
    while (-not $client.HasExited) {
        if ([datetime]::UtcNow -gt $deadline) { throw 'client_timeout' }
        if ($server.HasExited -and $server.ExitCode -ne 0) { throw 'server_failed' }
        Start-Sleep -Milliseconds 250
        $client.Refresh()
        $server.Refresh()
    }
    $client.WaitForExit()
    if ($client.ExitCode -ne 0) { throw 'client_failed' }
    $stage = 'server_completion'
    if (-not $server.WaitForExit(10000)) { throw 'server_completion_timeout' }
    if ($server.ExitCode -ne 0) { throw 'server_failed' }

    $stage = 'evidence_verification'
    $serverReport = Get-Content -Raw -LiteralPath (Join-Path $outputPath 'server.json') | ConvertFrom-Json -AsHashtable
    $clientReport = Get-Content -Raw -LiteralPath (Join-Path $outputPath 'client-events.json') | ConvertFrom-Json -AsHashtable
    Assert-LocalEvidence $serverReport $clientReport $env:RDP_TEST_MODE
    $outcome = 'passed'
} catch {
    $reason = 'operation_failed'
    $allowed = @('invalid_local_configuration', 'executables_not_built',
        'server_start_failed', 'server_ready_timeout', 'invalid_local_endpoint', 'invalid_fixture_certificate',
        'fixture_certificate_already_present', 'client_timeout', 'client_failed', 'server_failed',
        'server_completion_timeout', 'server_evidence_failed', 'client_evidence_failed', 'client_duration_failed')
    if ($_.Exception.Message -cin $allowed) { $reason = $_.Exception.Message }
    Write-Host ('Windows-only RDP test failed at stage: ' + $stage + ' (' + $reason + ')')
} finally {
    foreach ($process in @($client, $server)) {
        try { Stop-LocalProcess $process }
        catch { $outcome = 'failed'; $reason = 'process_cleanup_failed' }
    }
    try {
        if ($trustAdded) {
            $store.Remove($certificate)
            if ($store.Certificates.Find('FindByThumbprint', $certificate.Thumbprint, $false).Count -ne 0) {
                throw 'certificate_cleanup_failed'
            }
        }
    } catch { $outcome = 'failed'; $reason = 'certificate_cleanup_failed' }
    finally {
        if ($null -ne $store) { $store.Dispose() }
        if ($null -ne $certificate) { $certificate.Dispose() }
    }
    if ($null -ne $statePath) {
        # Delete only named fixture outputs in the unique directory we created.
        # Refuse to recursively delete arbitrary contents or a caller's directory.
        try {
            foreach ($name in @('ready.json', 'server.cer', 'desktop.rdp')) {
                $path = Join-Path $statePath $name
                if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
            }
            [IO.Directory]::Delete($statePath)
        } catch { $outcome = 'failed'; $reason = 'state_cleanup_failed' }
    }
    Export-TestEvents $started (Join-Path $outputPath 'windows-events.json')
    @{
        schema = 1; testKind = 'windows-local-rdp'; outcome = $outcome; stage = $stage; reason = $reason
        mode = $env:RDP_TEST_MODE; startedAt = $started.ToString('o'); finishedAt = [datetime]::UtcNow.ToString('o')
        harnessCommit = $env:GITHUB_SHA; testsKVM = $false; testsGatewayAuthentication = $false
    } | ConvertTo-Json | Set-Content (Join-Path $outputPath 'result.json') -Encoding utf8
}
if ($outcome -ne 'passed') { exit 1 }
Write-Host 'Windows-only RDP rendering, F8 input, 30-second session, and actual encoder checks passed.'
