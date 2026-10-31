Set-StrictMode -Version Latest

function Get-LocalClient([string] $Client, [string] $Workload) {
    if ([string]::IsNullOrEmpty($Client)) { return 'activex' }
    if ($Client -cnotin @('activex', 'mstsc') -or ($Client -ceq 'mstsc' -and $Workload -cne 'smoke')) {
        throw 'invalid_local_client'
    }
    return $Client
}

function New-MstscProfile($Ready) {
    Assert-LocalReady $Ready $Ready.mode
    if ($Ready.mode -cnotin @('bitmap', 'h264') -or $Ready.workload -cne 'smoke' -or
        $Ready.width -ne 1024 -or $Ready.height -ne 768) { throw 'invalid_mstsc_fixture' }
    # The fixture has no user authentication. Disable CredSSP only in this
    # loopback profile, retaining TLS and requiring trusted server authentication.
    # https://learn.microsoft.com/azure/virtual-desktop/rdp-properties
    return (@(
        ('full address:s:' + $Ready.address), 'username:s:rdp-smoke',
        'screen mode id:i:1', 'use multimon:i:0', 'desktopwidth:i:1024', 'desktopheight:i:768',
        'session bpp:i:32', 'authentication level:i:1', 'enablecredsspsupport:i:0',
        'negotiate security layer:i:1', 'prompt for credentials:i:0', 'prompt for credentials on client:i:0',
        'autoreconnection enabled:i:0', 'redirectclipboard:i:0', 'redirectprinters:i:0',
        'redirectcomports:i:0', 'redirectsmartcards:i:0', 'redirectwebauthn:i:0', 'drivestoredirect:s:',
        'audiomode:i:2', 'audiocapturemode:i:0', 'smart sizing:i:1', 'dynamic resolution:i:0',
        'disableconnectionsharing:i:1'
    ) -join "`r`n") + "`r`n"
}

function Assert-MstscEvidence($Server, $Client, [string] $Mode) {
    Assert-LocalEvidence $Server $Client $Mode
    # An ActiveX report cannot stand in for an mstsc.exe run. Also require
    # termination of the exact client that the runner launched.
    $started = @($Client.events | Where-Object {
        $_.stage -ceq 'client' -and $_.reason -ceq 'mstsc_process_started' -and $_.outcome -ceq 'passed'
    })
    $exited = @($Client.events | Where-Object {
        $_.stage -ceq 'client' -and $_.reason -ceq 'mstsc_process_exited' -and $_.outcome -ceq 'passed'
    })
    if ($started.Count -ne 1 -or $exited.Count -ne 1 -or
        $started[0].details.client -cne 'mstsc.exe' -or $exited[0].details.client -cne 'mstsc.exe' -or
        ($started[0].details.processId -isnot [int] -and $started[0].details.processId -isnot [long]) -or
        $started[0].details.processId -le 0 -or $exited[0].details.processId -ne $started[0].details.processId) {
        throw 'mstsc_evidence_failed'
    }
}
