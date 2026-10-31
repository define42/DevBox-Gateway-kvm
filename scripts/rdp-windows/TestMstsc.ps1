. (Join-Path $PSScriptRoot 'Mstsc.ps1')

Assert-True ((Get-LocalClient $null 'smoke') -ceq 'activex') 'existing client remains the default'
Assert-True ((Get-LocalClient 'activex' 'performance') -ceq 'activex') 'performance keeps the existing client'
Assert-True ((Get-LocalClient 'mstsc' 'smoke') -ceq 'mstsc') 'explicit executable client'
foreach ($invalid in @('MSTSC', ' mstsc', 'other')) {
    Assert-Rejected { Get-LocalClient $invalid 'smoke' | Out-Null } 'invalid client cannot silently fall back'
}
Assert-Rejected { Get-LocalClient 'mstsc' 'performance' | Out-Null } 'executable cannot claim ActiveX performance coverage'

$mstscReady = @{ address = '127.0.0.1:54321'; port = 54321; mode = 'bitmap'
    workload = 'smoke'; width = 1024; height = 768 }
$mstscProfile = New-MstscProfile $mstscReady
$mstscFields = @{}
foreach ($line in ($mstscProfile -split '\r?\n' | Where-Object { $_ -ne '' })) {
    $parts = $line -split ':', 3
    Assert-True ($parts.Count -eq 3 -and -not $mstscFields.ContainsKey($parts[0])) 'valid unique profile fields'
    $mstscFields[$parts[0]] = $parts[1] + ':' + $parts[2]
}
foreach ($field in @{
    'full address' = 's:127.0.0.1:54321'; 'username' = 's:rdp-smoke'
    'authentication level' = 'i:1'; 'enablecredsspsupport' = 'i:0'; 'negotiate security layer' = 'i:1'
    'prompt for credentials' = 'i:0'; 'prompt for credentials on client' = 'i:0'
    'desktopwidth' = 'i:1024'; 'desktopheight' = 'i:768'; 'screen mode id' = 'i:1'
    'autoreconnection enabled' = 'i:0'; 'disableconnectionsharing' = 'i:1'
    'redirectclipboard' = 'i:0'; 'redirectprinters' = 'i:0'; 'redirectwebauthn' = 'i:0'; 'drivestoredirect' = 's:'
    'audiomode' = 'i:2'; 'audiocapturemode' = 'i:0'; 'smart sizing' = 'i:1'; 'dynamic resolution' = 'i:0'
}.GetEnumerator()) {
    Assert-True ($mstscFields[$field.Key] -ceq $field.Value) ('mstsc profile: ' + $field.Key)
}
Assert-True (-not $mstscFields.ContainsKey('loadbalanceinfo') -and
             -not $mstscFields.ContainsKey('password 51')) 'no routing grant or saved credential'
foreach ($change in @(@{ Key = 'address'; Value = 'remote.example:54321' },
                     @{ Key = 'address'; Value = "127.0.0.1:54321`r`nredirectclipboard:i:1" },
                     @{ Key = 'port'; Value = 0 }, @{ Key = 'mode'; Value = 'invalid' },
                     @{ Key = 'workload'; Value = 'performance' }, @{ Key = 'width'; Value = 1920 })) {
    $badMstscReady = $mstscReady.Clone()
    $badMstscReady[$change.Key] = $change.Value
    Assert-Rejected { New-MstscProfile $badMstscReady | Out-Null } 'only the fixed loopback fixture may launch mstsc'
}

$mstscServer = @{ version = 1; mode = 'bitmap'; outcome = 'completed'; actualEncoder = 'bitmap'
    framesSent = 60; inputTransitions = 3; sessionSeconds = 31; graphicsReady = $false }
$mstscEvents = @(
    @{ stage = 'client'; outcome = 'passed'; reason = 'mstsc_process_started'
       details = @{ client = 'mstsc.exe'; processId = 123 } },
    @{ stage = 'render'; outcome = 'passed'; reason = 'fixture_visible' },
    @{ stage = 'input'; outcome = 'progress'; reason = 'f8_submitted'; details = @{ phase = 'initial' } },
    @{ stage = 'input'; outcome = 'passed'; reason = 'fixture_changed' },
    @{ stage = 'input'; outcome = 'progress'; reason = 'f8_submitted'; details = @{ phase = 'shrink' } },
    @{ stage = 'resize'; outcome = 'passed'; reason = 'window_resize_verified'
       details = @{ phase = 'shrink'; beforeWidth = 1040; beforeHeight = 807; windowWidth = 800; windowHeight = 600
           visibleWidth = 800; visibleHeight = 577; layoutBefore = 1; layoutAfter = 0
           heartbeatChanges = 4; lastChangeAgeSeconds = 0.5 } },
    @{ stage = 'input'; outcome = 'progress'; reason = 'f8_submitted'; details = @{ phase = 'restore' } },
    @{ stage = 'resize'; outcome = 'passed'; reason = 'window_resize_verified'
       details = @{ phase = 'restore'; beforeWidth = 800; beforeHeight = 600; windowWidth = 1040; windowHeight = 807
           visibleWidth = 1024; visibleHeight = 768; layoutBefore = 0; layoutAfter = 1
           heartbeatChanges = 4; lastChangeAgeSeconds = 0.5 } },
    @{ stage = 'render'; outcome = 'passed'; reason = 'heartbeat_verified'
       details = @{ changes = 20; lastChangeAgeSeconds = 0.5 } },
    @{ stage = 'session'; outcome = 'passed'; reason = 'render_input_and_duration_verified'
       details = @{ visibleSeconds = 30; requiredSeconds = 30 } },
    @{ stage = 'client'; outcome = 'passed'; reason = 'mstsc_process_exited'
       details = @{ client = 'mstsc.exe'; processId = 123 } }
)
$mstscReport = @{ version = 1; events = $mstscEvents }
function Copy-MstscEvidence {
    return ($mstscReport | ConvertTo-Json -Depth 8 | ConvertFrom-Json)
}
Assert-MstscEvidence $mstscServer $mstscReport 'bitmap'
Assert-MstscEvidence ($mstscServer | ConvertTo-Json | ConvertFrom-Json) (Copy-MstscEvidence) 'bitmap'
$mstscWithRequests = Copy-MstscEvidence
$mstscWithRequests.events = @($mstscEvents[0..3]) + @(
    @{ stage = 'resize'; outcome = 'progress'; reason = 'window_resize_requested'; details = @{ phase = 'shrink' } }
) + @($mstscEvents[4..5]) + @(
    @{ stage = 'resize'; outcome = 'progress'; reason = 'window_resize_requested'; details = @{ phase = 'restore' } }
) + @($mstscEvents[6..10])
Assert-MstscEvidence $mstscServer $mstscWithRequests 'bitmap'
foreach ($missing in 0..($mstscEvents.Count - 1)) {
    $events = for ($i = 0; $i -lt $mstscEvents.Count; $i++) { if ($i -ne $missing) { $mstscEvents[$i] } }
    Assert-Rejected { Assert-MstscEvidence $mstscServer @{ version = 1; events = @($events) } 'bitmap' } 'incomplete executable evidence rejected'
}
foreach ($change in @(@{ client = 'activex'; processId = 123 }, @{ client = 'mstsc.exe'; processId = 456 },
                     @{ client = 'mstsc.exe'; processId = 0 })) {
    $badReport = Copy-MstscEvidence
    $badReport.events[10].details = $change
    Assert-Rejected { Assert-MstscEvidence $mstscServer $badReport 'bitmap' } 'wrong executable or process cannot pass'
}
foreach ($case in @(
    @{ Name = 'duplicate resize'; Change = { param($r) $r.events += @($r.events[5]) } },
    @{ Name = 'reordered resize'; Change = { param($r) $r.events[5], $r.events[7] = $r.events[7], $r.events[5] } },
    @{ Name = 'duplicate input'; Change = { param($r) $r.events += @($r.events[4]) } },
    @{ Name = 'reordered input'; Change = { param($r) $r.events[4], $r.events[6] = $r.events[6], $r.events[4] } },
    @{ Name = 'wrong resize phase'; Change = { param($r) $r.events[5].details.phase = 'restore' } },
    @{ Name = 'unverified resize'; Change = { param($r) $r.events[5].outcome = 'progress' } },
    @{ Name = 'wrong resize reason'; Change = { param($r) $r.events[7].reason = 'window_requested' } },
    @{ Name = 'stale shrink'; Change = { param($r) $r.events[5].details.lastChangeAgeSeconds = 5.1 } },
    @{ Name = 'stale restore'; Change = { param($r) $r.events[7].details.lastChangeAgeSeconds = 5.1 } },
    @{ Name = 'negative heartbeat age'; Change = { param($r) $r.events[5].details.lastChangeAgeSeconds = -0.1 } },
    @{ Name = 'nonfinite heartbeat age'; Change = { param($r) $r.events[5].details.lastChangeAgeSeconds = [double]::NaN } },
    @{ Name = 'insufficient shrink heartbeat'; Change = { param($r) $r.events[5].details.heartbeatChanges = 3 } },
    @{ Name = 'insufficient restore heartbeat'; Change = { param($r) $r.events[7].details.heartbeatChanges = 3 } },
    @{ Name = 'no width change'; Change = { param($r) $r.events[5].details.windowWidth = 1040 } },
    @{ Name = 'no height change'; Change = { param($r) $r.events[5].details.windowHeight = 807 } },
    @{ Name = 'wrong restored width'; Change = { param($r) $r.events[7].details.windowWidth = 1041 } },
    @{ Name = 'wrong restored height'; Change = { param($r) $r.events[7].details.windowHeight = 808 } },
    @{ Name = 'broken width chain'; Change = { param($r) $r.events[7].details.beforeWidth = 801 } },
    @{ Name = 'broken height chain'; Change = { param($r) $r.events[7].details.beforeHeight = 601 } },
    @{ Name = 'unchanged shrink layout'; Change = { param($r) $r.events[5].details.layoutAfter = 1 } },
    @{ Name = 'unchanged restore layout'; Change = { param($r) $r.events[7].details.layoutAfter = 0 } },
    @{ Name = 'broken layout chain'; Change = { param($r) $r.events[7].details.layoutBefore = 1; $r.events[7].details.layoutAfter = 0 } },
    @{ Name = 'invalid layout'; Change = { param($r) $r.events[5].details.layoutBefore = 2 } },
    @{ Name = 'narrow render'; Change = { param($r) $r.events[5].details.visibleWidth = 639 } },
    @{ Name = 'short render'; Change = { param($r) $r.events[5].details.visibleHeight = 479 } },
    @{ Name = 'render wider than window'; Change = { param($r) $r.events[5].details.visibleWidth = 801 } },
    @{ Name = 'render taller than window'; Change = { param($r) $r.events[5].details.visibleHeight = 601 } },
    @{ Name = 'unchanged render width'; Change = { param($r) $r.events[7].details.visibleWidth = 800 } },
    @{ Name = 'unchanged render height'; Change = { param($r) $r.events[7].details.visibleHeight = 577 } },
    @{ Name = 'string dimension'; Change = { param($r) $r.events[5].details.windowWidth = '800' } },
    @{ Name = 'missing detail'; Change = { param($r) $r.events[5].details.PSObject.Properties.Remove('heartbeatChanges') } }
)) {
    $badReport = Copy-MstscEvidence
    & $case.Change $badReport
    Assert-Rejected { Assert-MstscEvidence $mstscServer $badReport 'bitmap' } ('invalid resize evidence: ' + $case.Name)
}
foreach ($inputCount in 0..2) {
    $badServer = $mstscServer.Clone()
    $badServer.inputTransitions = $inputCount
    Assert-Rejected { Assert-MstscEvidence $badServer $mstscReport 'bitmap' } 'all three F8 inputs must reach the server'
}
$events = $mstscEvents + @(@{ stage = 'cleanup'; outcome = 'failed'; reason = 'process_cleanup_failed' })
Assert-Rejected { Assert-MstscEvidence $mstscServer @{ version = 1; events = $events } 'bitmap' } 'cleanup failure overrides successful render'
