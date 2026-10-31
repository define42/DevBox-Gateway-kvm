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
    $inputs = @($Client.events | Where-Object {
        $_.stage -ceq 'input' -and $_.outcome -ceq 'progress' -and $_.reason -ceq 'f8_submitted'
    })
    if ($inputs.Count -ne 3 -or $inputs[0].details.phase -cne 'initial' -or
        $inputs[1].details.phase -cne 'shrink' -or $inputs[2].details.phase -cne 'restore' -or
        ($Server.inputTransitions -isnot [int] -and $Server.inputTransitions -isnot [long]) -or
        $Server.inputTransitions -lt 3) { throw 'mstsc_resize_input_evidence_failed' }

    $resizes = @($Client.events | Where-Object {
        $_.stage -ceq 'resize' -and $_.reason -ceq 'window_resize_verified'
    })
    if ($resizes.Count -ne 2) { throw 'mstsc_resize_evidence_failed' }
    for ($i = 0; $i -lt 2; $i++) {
        $event = $resizes[$i]
        $details = $event.details
        if ($event.outcome -cne 'passed' -or $event.reason -cne 'window_resize_verified' -or
            $details.phase -cne @('shrink', 'restore')[$i]) { throw 'mstsc_resize_evidence_failed' }
        foreach ($field in @('beforeWidth', 'beforeHeight', 'windowWidth', 'windowHeight',
                             'visibleWidth', 'visibleHeight', 'heartbeatChanges')) {
            $value = $details.$field
            if (($value -isnot [int] -and $value -isnot [long]) -or $value -le 0) {
                throw 'mstsc_resize_evidence_failed'
            }
        }
        foreach ($field in @('layoutBefore', 'layoutAfter')) {
            $value = $details.$field
            if (($value -isnot [int] -and $value -isnot [long]) -or $value -notin @(0, 1)) {
                throw 'mstsc_resize_evidence_failed'
            }
        }
        $age = $details.lastChangeAgeSeconds
        if (($age -isnot [int] -and $age -isnot [long] -and $age -isnot [double] -and $age -isnot [decimal]) -or
            [double]::IsNaN($age) -or [double]::IsInfinity($age) -or $age -lt 0 -or $age -gt 5 -or
            $details.heartbeatChanges -lt 4 -or $details.layoutAfter -ne (1 - $details.layoutBefore) -or
            $details.visibleWidth -lt 640 -or $details.visibleHeight -lt 480 -or
            $details.visibleWidth -gt $details.windowWidth -or $details.visibleHeight -gt $details.windowHeight) {
            throw 'mstsc_resize_evidence_failed'
        }
    }
    $shrink = $resizes[0].details
    $restore = $resizes[1].details
    if ($shrink.windowWidth -ge $shrink.beforeWidth -or $shrink.windowHeight -ge $shrink.beforeHeight -or
        $restore.beforeWidth -ne $shrink.windowWidth -or $restore.beforeHeight -ne $shrink.windowHeight -or
        $restore.windowWidth -ne $shrink.beforeWidth -or $restore.windowHeight -ne $shrink.beforeHeight -or
        $restore.layoutBefore -ne $shrink.layoutAfter -or
        $restore.visibleWidth -le $shrink.visibleWidth -or $restore.visibleHeight -le $shrink.visibleHeight) {
        throw 'mstsc_resize_evidence_failed'
    }
}
