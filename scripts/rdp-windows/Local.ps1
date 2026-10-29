Set-StrictMode -Version Latest

function Assert-LocalReady($Ready, [string] $Mode) {
    if ($Ready.mode -cne $Mode -or ($Ready.port -isnot [long] -and $Ready.port -isnot [int]) -or
        $Ready.port -lt 1 -or $Ready.port -gt 65535 -or
        $Ready.address -cne ('127.0.0.1:' + $Ready.port)) { throw 'invalid_local_endpoint' }
}

function New-LocalProfile($Ready) {
    # The parser uses the shipped profile schema, but this token is a dummy and
    # is NOT transmitted: Client.cs local mode preserves the default RDP cookie.
    $token = '0' * 64
    return (@(
        ('full address:s:' + $Ready.address), 'username:s:rdp-smoke', ('loadbalanceinfo:s:' + $token),
        'screen mode id:i:2', 'use multimon:i:0', 'session bpp:i:32', 'authentication level:i:2',
        'enablecredsspsupport:i:1', 'negotiate security layer:i:1', 'prompt for credentials:i:1',
        'prompt for credentials on client:i:1', 'autoreconnection enabled:i:0', 'redirectclipboard:i:0',
        'audiomode:i:2', 'audiocapturemode:i:0'
    ) -join "`r`n") + "`r`n"
}

function Assert-LocalEvidence($Server, $Client, [string] $Mode) {
    $encoder = if ($Mode -eq 'bitmap') { 'bitmap' } else { 'software' }
    if ($Server.version -ne 1 -or $Server.mode -cne $Mode -or $Server.outcome -cne 'completed' -or
        $Server.actualEncoder -cne $encoder -or $Server.framesSent -lt 1 -or
        $Server.inputTransitions -lt 1 -or $Server.sessionSeconds -lt 30 -or
        ($Mode -eq 'h264' -and $Server.graphicsReady -ne $true)) {
        throw 'server_evidence_failed'
    }
    if ($Client.version -ne 1 -or @($Client.events | Where-Object { $_.outcome -eq 'failed' }).Count -gt 0) {
        throw 'client_evidence_failed'
    }
    foreach ($requirement in @(
        @{ Stage = 'render'; Reason = 'fixture_visible' },
        @{ Stage = 'input'; Reason = 'fixture_changed' },
        @{ Stage = 'render'; Reason = 'heartbeat_verified' },
        @{ Stage = 'session'; Reason = 'render_input_and_duration_verified' }
    )) {
        $matches = @($Client.events | Where-Object {
            $_.stage -ceq $requirement.Stage -and $_.reason -ceq $requirement.Reason -and $_.outcome -ceq 'passed'
        })
        if ($matches.Count -ne 1) { throw 'client_evidence_failed' }
        if ($requirement.Reason -eq 'heartbeat_verified' -and
            ($matches[0].details.changes -lt 4 -or $matches[0].details.lastChangeAgeSeconds -lt 0 -or
             $matches[0].details.lastChangeAgeSeconds -gt 5)) { throw 'client_evidence_failed' }
        if ($requirement.Stage -eq 'session' -and
            ($matches[0].details.visibleSeconds -lt 30 -or $matches[0].details.requiredSeconds -lt 30)) {
            throw 'client_duration_failed'
        }
    }
}

function Stop-LocalProcess($Process) {
    if ($null -eq $Process) { return }
    try {
        if (-not $Process.HasExited) {
            $Process.Kill()
            if (-not $Process.WaitForExit(5000)) { throw 'process_cleanup_timeout' }
        }
    } finally { $Process.Dispose() }
}
