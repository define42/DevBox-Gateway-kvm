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
    framesSent = 60; inputTransitions = 1; sessionSeconds = 31; graphicsReady = $false }
$mstscEvents = @(
    @{ stage = 'render'; outcome = 'passed'; reason = 'fixture_visible' },
    @{ stage = 'input'; outcome = 'passed'; reason = 'fixture_changed' },
    @{ stage = 'render'; outcome = 'passed'; reason = 'heartbeat_verified'
       details = @{ changes = 20; lastChangeAgeSeconds = 0.5 } },
    @{ stage = 'session'; outcome = 'passed'; reason = 'render_input_and_duration_verified'
       details = @{ visibleSeconds = 30; requiredSeconds = 30 } },
    @{ stage = 'client'; outcome = 'passed'; reason = 'mstsc_process_started'
       details = @{ client = 'mstsc.exe'; processId = 123 } },
    @{ stage = 'client'; outcome = 'passed'; reason = 'mstsc_process_exited'
       details = @{ client = 'mstsc.exe'; processId = 123 } }
)
$mstscReport = @{ version = 1; events = $mstscEvents }
Assert-MstscEvidence $mstscServer $mstscReport 'bitmap'
foreach ($missing in 0..5) {
    $events = for ($i = 0; $i -lt $mstscEvents.Count; $i++) { if ($i -ne $missing) { $mstscEvents[$i] } }
    Assert-Rejected { Assert-MstscEvidence $mstscServer @{ version = 1; events = @($events) } 'bitmap' } 'incomplete executable evidence rejected'
}
foreach ($change in @(@{ client = 'activex'; processId = 123 }, @{ client = 'mstsc.exe'; processId = 456 },
                     @{ client = 'mstsc.exe'; processId = 0 })) {
    $badExit = $mstscEvents[5].Clone()
    $badExit.details = $change
    $events = @($mstscEvents[0..4]) + @($badExit)
    Assert-Rejected { Assert-MstscEvidence $mstscServer @{ version = 1; events = $events } 'bitmap' } 'wrong executable or process cannot pass'
}
$events = $mstscEvents + @(@{ stage = 'cleanup'; outcome = 'failed'; reason = 'process_cleanup_failed' })
Assert-Rejected { Assert-MstscEvidence $mstscServer @{ version = 1; events = $events } 'bitmap' } 'cleanup failure overrides successful render'
