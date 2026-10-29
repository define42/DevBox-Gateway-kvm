# Exercise real child termination without changing certificate trust or needing
# Windows. Keep all fixtures scoped away from the rest of Test.ps1.
& {
    $directory = Join-Path ([IO.Path]::GetTempPath()) ('rdp process test ' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($directory)
    $childPath = Join-Path $directory 'child.ps1'
    $markerPath = Join-Path $directory 'started.json'
    try {
        [IO.File]::WriteAllText($childPath, @'
param([string] $MarkerPath)
[IO.File]::WriteAllText($MarkerPath, [string]$PID)
Start-Sleep -Seconds 60
'@)
        $launch = [Diagnostics.ProcessStartInfo]::new((Get-Process -Id $PID).Path)
        $launch.UseShellExecute = $false
        foreach ($argument in @('-NoLogo', '-NoProfile', '-NonInteractive', '-File', $childPath, '-MarkerPath', $markerPath)) {
            [void]$launch.ArgumentList.Add($argument)
        }
        $timer = [Diagnostics.Stopwatch]::StartNew()
        $reason = $null
        try { Invoke-LocalBoundedProcess $launch 5000 | Out-Null }
        catch { $reason = $_.Exception.Message }
        Assert-True ($reason -ceq 'fixture_certificate_timeout') 'blocked helper times out'
        Assert-True ($timer.Elapsed.TotalSeconds -lt 12) 'timeout and process cleanup are bounded'
        Assert-True ([IO.File]::Exists($markerPath)) 'child reached operation before timeout'
        $childID = [int][IO.File]::ReadAllText($markerPath)
        $running = Get-Process -Id $childID -ErrorAction SilentlyContinue
        Assert-True ($null -eq $running) 'timed out helper has exited before cleanup'

        [IO.File]::WriteAllText($childPath, 'exit 7')
        Assert-True ((Invoke-LocalBoundedProcess $launch) -eq 7) 'child exit failure is preserved'

        Write-LocalProgress $directory 'temporary_certificate_trust'
        $progress = [IO.File]::ReadAllText((Join-Path $directory 'progress.json')) | ConvertFrom-Json
        Assert-True ($progress.stage -ceq 'temporary_certificate_trust') 'progress saved before helper starts'
        Assert-True (-not [IO.File]::Exists((Join-Path $directory 'progress.json.tmp'))) 'progress published atomically'
        $errors = [Collections.Generic.List[string]]::new()
        Write-LocalCleanupProgress (Join-Path $directory 'missing') 'certificate_cleanup' $errors
        Assert-True ($errors.Contains('progress_write_failed')) 'failed progress write cannot abort cleanup'

        # Failure to confirm child termination must never call Remove, even when
        # ownership was recorded. A completed child still permits normal cleanup.
        function Invoke-LocalCertificateTrust { throw 'should_not_be_called' }
        [IO.File]::WriteAllText((Join-Path $directory 'trust-receipt.json'), 'owned')
        $errors.Clear()
        Assert-True (-not (Clear-LocalCertificateTrust $directory $false $errors)) 'running installer prevents cleanup race'
        Assert-True ($errors.Count -eq 1 -and $errors[0] -ceq 'certificate_child_cleanup_failed') 'unconfirmed helper exit reported'
        Assert-True ([IO.File]::ReadAllText((Join-Path $directory 'trust-receipt.json')) -ceq 'owned') 'ownership preserved for unresolved installer'
        $errors.Clear()
        Assert-True (-not (Clear-LocalCertificateTrust $directory $true $errors)) 'failed removal cannot pass cleanup'
        Assert-True ($errors.Count -eq 1 -and $errors[0] -ceq 'certificate_cleanup_failed') 'removal failure reported'
        Remove-Item -LiteralPath (Join-Path $directory 'trust-receipt.json')
        Assert-True (Clear-LocalCertificateTrust $directory $true $errors) 'no owned certificate needs no helper'
    } finally {
        foreach ($name in @('child.ps1', 'started.json', 'progress.json', 'progress.json.tmp', 'trust-receipt.json')) {
            [IO.File]::Delete((Join-Path $directory $name))
        }
        [IO.Directory]::Delete($directory)
    }
}
