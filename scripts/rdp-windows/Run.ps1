$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Gateway.ps1')

$stage = 'configuration'
$outcome = 'failed'
$failureReason = $null
$started = [datetime]::UtcNow
$client = $null
$handler = $null
$process = $null
$profilePath = $null
$origin = $null
$csrf = $null
$lastReport = $null
$observedEncoder = $false
$outputPath = $env:RDP_TEST_OUTPUT
if ([string]::IsNullOrWhiteSpace($outputPath)) { throw 'RDP_TEST_OUTPUT is required.' }
[void][IO.Directory]::CreateDirectory($outputPath)

try {
    $origin = Get-TestOrigin $env:RDP_TEST_URL
    if ($env:RDP_TEST_MODE -notin @('bitmap', 'h264') -or
        $env:RDP_TEST_VM_ID -cnotmatch '^[a-z0-9]{1,64}$' -or
        [string]::IsNullOrEmpty($env:RDP_TEST_USERNAME) -or
        [string]::IsNullOrEmpty($env:RDP_TEST_PASSWORD)) { throw 'missing_test_configuration' }
    $executable = Join-Path $env:RUNNER_TEMP 'rdp-client.exe'
    if (-not (Test-Path $executable)) { throw 'client_not_built' }
    $handler = [System.Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $handler.UseProxy = $false # HTTP and RDP must use the same source IP.
    $handler.CookieContainer = [System.Net.CookieContainer]::new()
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromSeconds(15)
    $client.MaxResponseContentBufferSize = 4MB

    $stage = 'http_login'
    $login = Invoke-TestHTTP $client $origin '/login' $null
    if ($login.Status -ne 200) { throw 'login_page_unavailable' }
    $loginCSRF = Get-TestCSRF $login.Body
    $login = Invoke-TestHTTP $client $origin '/login' @{
        csrf = $loginCSRF; username = $env:RDP_TEST_USERNAME; password = $env:RDP_TEST_PASSWORD
    }
    if ($login.Status -ne 303 -or $login.Location -ne '/') { throw 'login_failed' }
    $dashboard = Invoke-TestHTTP $client $origin '/' $null
    if ($dashboard.Status -ne 200) { throw 'dashboard_unavailable' }
    $csrf = Get-TestCSRF $dashboard.Body

    $stage = 'target_preflight'
    $lastReport = Get-TestReport $client $origin $env:RDP_TEST_VM_ID
    $lastReport | ConvertTo-Json -Depth 32 | Set-Content (Join-Path $outputPath 'gateway-before.json') -Encoding utf8
    $expected = if ($env:RDP_TEST_MODE -eq 'bitmap') { 'bitmap' } else { 'software' }
    $performance = $lastReport.vms[0].performance
    if ($lastReport.vms[0].state -ne 'running' -or $performance.state -ne 'live' -or
        $performance.requestedEncoder -ne $expected -or $performance.connections -ne 0) {
        throw 'target_not_ready_or_wrong_mode'
    }

    $stage = 'profile_download'
    $profile = Invoke-TestHTTP $client $origin ('/vms/' + $env:RDP_TEST_VM_ID + '/rdp') @{ csrf = $csrf }
    if ($profile.Status -ne 200 -or $profile.ContentType -notlike 'application/x-rdp*') { throw 'download_failed' }
    Assert-TestProfile $profile.Body $origin $env:RDP_TEST_USERNAME
    $profilePath = Join-Path $env:RUNNER_TEMP ([IO.Path]::GetRandomFileName() + '.rdp')
    [IO.File]::WriteAllText($profilePath, $profile.Body)
    $profile = $null

    $stage = 'microsoft_client'
    $env:RDP_SMOKE_PROFILE = $profilePath
    $env:RDP_SMOKE_PASSWORD = $env:RDP_TEST_PASSWORD
    $env:RDP_SMOKE_OUTPUT = $outputPath
    $env:RDP_SMOKE_LOCAL = '0'
    $env:RDP_SMOKE_DURATION_SECONDS = '30'
    $connectedSince = [datetime]::UtcNow
    # Password is inherited in the environment, never a command-line argument.
    # Remove the original secret before spawning, so the child gets one copy.
    $env:RDP_TEST_PASSWORD = $null
    $process = Start-Process -FilePath $executable -PassThru -NoNewWindow
    $env:RDP_SMOKE_PASSWORD = $null
    $deadline = [datetime]::UtcNow.AddSeconds(150)
    while (-not $process.HasExited) {
        if ([datetime]::UtcNow -gt $deadline) { throw 'client_timeout' }
        Start-Sleep -Seconds 2
        try {
            $lastReport = Get-TestReport $client $origin $env:RDP_TEST_VM_ID
            if (Test-ActiveEncoder $lastReport $env:RDP_TEST_MODE $connectedSince) {
                $observedEncoder = $true
                $lastReport | ConvertTo-Json -Depth 32 | Set-Content (Join-Path $outputPath 'gateway-active.json') -Encoding utf8
            }
        } catch { # Final diagnostics still run if a transient poll fails.
        }
        $process.Refresh()
    }
    $process.WaitForExit()
    if ($process.ExitCode -ne 0) { throw 'client_failed' }
    $stage = 'encoder_verification'
    if (-not $observedEncoder) { throw 'expected_active_encoder_not_observed' }
    $outcome = 'passed'
} catch {
    # Never print $_: HTTP/COM exceptions may embed credentials or routing data.
    $failureReason = 'operation_failed'
    $allowedReasons = @('invalid_test_origin', 'missing_test_configuration', 'client_not_built',
        'login_page_unavailable', 'missing_csrf', 'login_failed', 'dashboard_unavailable',
        'diagnostics_unavailable', 'unexpected_diagnostics_scope', 'target_not_ready_or_wrong_mode',
        'download_failed', 'invalid_rdp_profile', 'rdp_endpoint_mismatch', 'unexpected_rdp_profile',
        'client_timeout', 'client_failed', 'expected_active_encoder_not_observed')
    if ($_.Exception.Message -cin $allowedReasons) { $failureReason = $_.Exception.Message }
    Write-Host ('RDP compatibility test failed at stage: ' + $stage)
} finally {
    if ($null -ne $process) {
        try {
            if (-not $process.HasExited) { $process.Kill(); $process.WaitForExit() }
        } catch { Write-Host 'Client teardown failed; attempting remaining cleanup.' }
        finally { $process.Dispose() }
    }
    if ($null -ne $profilePath -and (Test-Path -LiteralPath $profilePath)) {
        try { Remove-Item -LiteralPath $profilePath -Force }
        catch {
            $outcome = 'failed'
            $failureReason = 'profile_cleanup_failed'
            Write-Host 'Temporary routing profile cleanup failed.'
        }
    }
    $env:RDP_SMOKE_PASSWORD = $null
    $env:RDP_TEST_PASSWORD = $null
    $env:RDP_SMOKE_PROFILE = $null
    $env:RDP_SMOKE_LOCAL = $null
    if ($null -ne $client -and $null -ne $csrf) {
        # Worker terminal histories arrive with the five-second heartbeat.
        Start-Sleep -Seconds 6
        try {
            $lastReport = Get-TestReport $client $origin $env:RDP_TEST_VM_ID
            $lastReport | ConvertTo-Json -Depth 32 | Set-Content (Join-Path $outputPath 'gateway-after.json') -Encoding utf8
        } catch { Write-Host 'Final gateway diagnostics unavailable.' }
        try { $null = Invoke-TestHTTP $client $origin '/logout' @{ csrf = $csrf } } catch { }
    }
    if ($null -ne $client) { $client.Dispose() }
    if ($null -ne $handler) { $handler.Dispose() }
    Export-TestEvents $started (Join-Path $outputPath 'windows-events.json')
    @{
        schema = 1; outcome = $outcome; stage = $stage; reason = $failureReason; mode = $env:RDP_TEST_MODE
        expectedActiveEncoderObserved = $observedEncoder
        startedAt = $started.ToString('o'); finishedAt = [datetime]::UtcNow.ToString('o')
        harnessCommit = $env:GITHUB_SHA
    } | ConvertTo-Json | Set-Content (Join-Path $outputPath 'result.json') -Encoding utf8
}
if ($outcome -ne 'passed') { exit 1 }
Write-Host 'Microsoft RDP rendered-desktop, input, session stability, and encoder checks passed.'
