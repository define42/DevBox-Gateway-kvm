$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Gateway.ps1')
. (Join-Path $PSScriptRoot 'Local.ps1')

function Assert-True([bool] $Condition, [string] $Label) {
    if (-not $Condition) { throw ('Assertion failed: ' + $Label) }
}
function Assert-Rejected([scriptblock] $Action, [string] $Label) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    Assert-True $rejected $Label
}

foreach ($file in @('Build.ps1', 'Gateway.ps1', 'Run.ps1', 'Local.ps1', 'RunLocal.ps1', 'Test.ps1',
                    'CertificateTrust.ps1', 'TestCertificateTrust.ps1', 'TestLocalProcess.ps1')) {
    $tokens = $null
    $errors = $null
    $null = [Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $file), [ref]$tokens, [ref]$errors)
    Assert-True ($errors.Count -eq 0) ($file + ' syntax')
}

. (Join-Path $PSScriptRoot 'TestCertificateTrust.ps1')
. (Join-Path $PSScriptRoot 'TestLocalProcess.ps1')

# Re-running with stale output must not overwrite previous evidence, even in
# failure cleanup. This check never starts a server or changes certificate trust.
$guardDirectory = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
$previousOutput = $env:RDP_TEST_OUTPUT
try {
    [void][IO.Directory]::CreateDirectory($guardDirectory)
    [IO.File]::WriteAllText((Join-Path $guardDirectory 'result.json'), 'previous evidence')
    $env:RDP_TEST_OUTPUT = $guardDirectory
    Assert-Rejected { & (Join-Path $PSScriptRoot 'RunLocal.ps1') } 'stale local evidence rejected'
    Assert-True (([IO.File]::ReadAllText((Join-Path $guardDirectory 'result.json'))) -ceq 'previous evidence') 'stale evidence preserved'
    Assert-True (@(Get-ChildItem -LiteralPath $guardDirectory -Force).Count -eq 1) 'no cleanup artifacts added to stale output'
} finally {
    $env:RDP_TEST_OUTPUT = $previousOutput
    [IO.File]::Delete((Join-Path $guardDirectory 'result.json'))
    [IO.Directory]::Delete($guardDirectory)
}

$origin = Get-TestOrigin 'https://gateway.test:8443'
Assert-True ($origin.Port -eq 8443) 'origin port'
foreach ($invalid in @('http://gateway.test', 'https://user:pass@gateway.test',
                       'https://gateway.test/path', 'https://gateway.test/?q=a',
                       'https://gateway.test/#fragment', 'not a URL')) {
    Assert-Rejected { Get-TestOrigin $invalid | Out-Null } 'reject unsafe origin'
}
$token = 'a' * 64
Assert-True ((Get-TestCSRF ('<input type="hidden" name="csrf" value="' + $token + '">')) -eq $token) 'csrf token'
Assert-Rejected { Get-TestCSRF '<input name="csrf" value="bad">' | Out-Null } 'bad csrf'
$profile = @"
full address:s:gateway.test:8443
username:s:testuser
loadbalanceinfo:s:$token
authentication level:i:2
enablecredsspsupport:i:1
negotiate security layer:i:1
autoreconnection enabled:i:0
"@
Assert-TestProfile $profile $origin 'testuser'
foreach ($change in @(
    @{ From = 'gateway.test:8443'; To = 'attacker.test:8443' },
    @{ From = 'gateway.test:8443'; To = 'gateway.test:443' },
    @{ From = 'username:s:testuser'; To = 'username:s:another' },
    @{ From = ('loadbalanceinfo:s:' + $token); To = 'loadbalanceinfo:s:bad' },
    @{ From = 'authentication level:i:2'; To = 'authentication level:i:0' },
    @{ From = 'enablecredsspsupport:i:1'; To = 'enablecredsspsupport:i:0' },
    @{ From = 'negotiate security layer:i:1'; To = 'negotiate security layer:i:0' },
    @{ From = 'autoreconnection enabled:i:0'; To = 'autoreconnection enabled:i:1' }
)) {
    $bad = $profile.Replace($change.From, $change.To)
    Assert-Rejected { Assert-TestProfile $bad $origin 'testuser' } 'reject changed profile'
}
Assert-Rejected { Assert-TestProfile ($profile + "`nfull address:s:gateway.test:8443") $origin 'testuser' } 'duplicate profile field'
Assert-TestProfile ($profile.Replace('gateway.test:8443', '[::1]:8443')) (Get-TestOrigin 'https://[::1]:8443') 'testuser'

$ready = @{ address = '127.0.0.1:54321'; port = 54321; mode = 'bitmap' }
Assert-LocalReady $ready 'bitmap'
$localProfile = New-LocalProfile $ready
Assert-TestProfile $localProfile (Get-TestOrigin 'https://127.0.0.1:54321') 'rdp-smoke'
Assert-Rejected { Assert-LocalReady $ready 'h264' } 'wrong ready mode'
foreach ($badReady in @(
    @{ address = 'remote.example:54321'; port = 54321; mode = 'bitmap' },
    @{ address = '127.0.0.1:54322'; port = 54321; mode = 'bitmap' },
    @{ address = '127.0.0.1:54321'; port = '54321'; mode = 'bitmap' },
    @{ address = '127.0.0.1:0'; port = 0; mode = 'bitmap' },
    @{ address = '127.0.0.1:65536'; port = 65536; mode = 'bitmap' }
)) { Assert-Rejected { Assert-LocalReady $badReady 'bitmap' } 'unsafe ready endpoint' }

$serverEvidence = @{ version = 1; mode = 'bitmap'; outcome = 'completed'; actualEncoder = 'bitmap';
    framesSent = 10; inputTransitions = 1; sessionSeconds = 31; graphicsReady = $false }
$clientEvidence = @{ version = 1; events = @(
    @{ stage = 'render'; outcome = 'passed'; reason = 'fixture_visible' },
    @{ stage = 'input'; outcome = 'passed'; reason = 'fixture_changed' },
    @{ stage = 'session'; outcome = 'passed'; reason = 'render_input_and_duration_verified';
       details = @{ visibleSeconds = 30; requiredSeconds = 30 } },
    @{ stage = 'render'; outcome = 'passed'; reason = 'heartbeat_verified';
       details = @{ changes = 20; lastChangeAgeSeconds = 0.25 } }
) }
Assert-LocalEvidence $serverEvidence $clientEvidence 'bitmap'
# Exercise the same JSON object types as the orchestrator, not only hashtables.
Assert-LocalEvidence (ConvertFrom-Json (ConvertTo-Json -Depth 10 $serverEvidence)) `
    (ConvertFrom-Json (ConvertTo-Json -Depth 10 $clientEvidence)) 'bitmap'
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'bitmap fallback cannot pass H264'
foreach ($change in @(
    @{ Key = 'framesSent'; Value = 0 }, @{ Key = 'inputTransitions'; Value = 0 },
    @{ Key = 'sessionSeconds'; Value = 29 }, @{ Key = 'outcome'; Value = 'failed' },
    @{ Key = 'actualEncoder'; Value = 'software' }, @{ Key = 'version'; Value = 0 }
)) {
    $badEvidence = $serverEvidence.Clone()
    $badEvidence[$change.Key] = $change.Value
    Assert-Rejected { Assert-LocalEvidence $badEvidence $clientEvidence 'bitmap' } 'incomplete server evidence'
}
$serverEvidence.mode = 'h264'
$serverEvidence.actualEncoder = 'software'
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'H264 must negotiate graphics'
$serverEvidence.graphicsReady = $true
Assert-LocalEvidence $serverEvidence $clientEvidence 'h264'
$missingInput = @{ version = 1; events = @($clientEvidence.events[0], $clientEvidence.events[2], $clientEvidence.events[3]) }
Assert-Rejected { Assert-LocalEvidence $serverEvidence $missingInput 'h264' } 'client must demonstrate input'
$missingHeartbeat = @{ version = 1; events = @($clientEvidence.events[0], $clientEvidence.events[1], $clientEvidence.events[2]) }
Assert-Rejected { Assert-LocalEvidence $serverEvidence $missingHeartbeat 'h264' } 'client must demonstrate continuing rendering'
$clientEvidence.events[3].details.lastChangeAgeSeconds = 6
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'stale heartbeat rejected'
$clientEvidence.events[3].details.lastChangeAgeSeconds = 0.25
$clientEvidence.events[3].details.changes = 0
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'static heartbeat rejected'
$clientEvidence.events[3].details.changes = 20
$clientEvidence.events[2].details.visibleSeconds = 1
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'client short duration rejected'
$clientEvidence.events[2].details.visibleSeconds = 30
$clientEvidence.events += @{ stage = 'connection'; outcome = 'failed'; reason = 'unexpected_disconnect' }
Assert-Rejected { Assert-LocalEvidence $serverEvidence $clientEvidence 'h264' } 'disconnect failure overrides successful events'

$since = [datetime]::UtcNow
$report = @{ vms = @(@{ performance = @{
    state = 'live'; connections = 1; encoder = 'software'; framesSent = 12; sampledAt = $since.ToString('o')
} }) }
Assert-True (Test-ActiveEncoder $report 'h264' $since.AddSeconds(-1)) 'fresh h264 sample'
Assert-True (-not (Test-ActiveEncoder $report 'bitmap' $since.AddSeconds(-1))) 'h264 is not bitmap'
Assert-True (-not (Test-ActiveEncoder $report 'h264' $since.AddSeconds(1))) 'old sample rejected'
$report.vms[0].performance.encoder = 'bitmap'
Assert-True (Test-ActiveEncoder $report 'bitmap' $since.AddSeconds(-1)) 'fresh bitmap sample'
$report.vms[0].performance.connections = 2
Assert-True (-not (Test-ActiveEncoder $report 'bitmap' $since.AddSeconds(-1))) 'multiple viewers rejected'
$report.vms[0].performance.connections = 0
Assert-True (-not (Test-ActiveEncoder $report 'bitmap' $since.AddSeconds(-1))) 'idle sample rejected'

# Test HTTP form encoding and explicit response handling entirely in memory.
Add-Type -TypeDefinition @'
using System;
using System.Net;
using System.Net.Http;
using System.Threading;
using System.Threading.Tasks;
public sealed class RDPSmokeTestHandler : HttpMessageHandler {
    public string Body;
    public string Method;
    public string Origin;
    public int Status = 303;
    public string ResponseBody = "";
    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage r, CancellationToken ct) {
        Method = r.Method.Method;
        Body = r.Content == null ? "" : await r.Content.ReadAsStringAsync();
        Origin = r.Headers.Contains("Origin") ? String.Join("", r.Headers.GetValues("Origin")) : "";
        var result = new HttpResponseMessage((HttpStatusCode)Status);
        result.Content = new StringContent(ResponseBody, System.Text.Encoding.UTF8, "application/json");
        result.Headers.Location = new Uri("/", UriKind.Relative);
        return result;
    }
}
'@
$handler = [RDPSmokeTestHandler]::new()
$client = [System.Net.Http.HttpClient]::new($handler)
try {
    $reply = Invoke-TestHTTP $client $origin '/login' @{ username = 'test'; password = 'a&b+c= d' }
    Assert-True ($reply.Status -eq 303 -and $reply.Location -eq '/') 'redirect returned explicitly'
    Assert-True ($handler.Method -eq 'POST' -and $handler.Origin -eq 'https://gateway.test:8443') 'POST origin'
    Assert-True ($handler.Body.Contains('password=a%26b%2Bc%3D+d')) 'encoded password'
    $handler.Status = 200
    $handler.ResponseBody = '{"scope":"current-user","vms":[{"id":"testvm"}]}'
    $result = Get-TestReport $client $origin 'testvm'
    Assert-True ($handler.Method -eq 'GET' -and $handler.Body -eq '' -and $result.vms.Count -eq 1) 'owner report'
    Assert-Rejected { Get-TestReport $client $origin 'wrongvm' | Out-Null } 'wrong report VM'
    $handler.Status = 303
    Assert-Rejected { Get-TestReport $client $origin 'testvm' | Out-Null } 'expired session redirect'
} finally { $client.Dispose() }

# Check event redaction without needing Windows event logs or saving raw XML.
$eventFixtureXML = @(
    '<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><EventData><Data Name="DisconnectReason">4868</Data><Data Name="ErrorCode">0x0</Data><Data Name="ServerName">private.example</Data><Data Name="UserName">123456</Data><Data Name="ErrorText">sensitive message</Data></EventData></Event>',
    '<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><UserData><EventXML xmlns="Event_NS"><Reason>4868</Reason><ErrorCode> 0x0 </ErrorCode><ServerName>private.example</ServerName><UserName>123456</UserName><ErrorText>sensitive message</ErrorText></EventXML></UserData></Event>',
    '<Event><EventData><Data>3334</Data></EventData></Event>',
    '<Event><System/><RenderingInfo><Message>sensitive message</Message><ErrorCode>0xdeadbeef</ErrorCode></RenderingInfo></Event>',
    '<Event><EventData>',
    '<Event><EventData><ComplexData><Data Name="reason">3334</Data><Data Name="errorCode">0x80004005</Data><Data Name="ErrorText">private.example</Data></ComplexData></EventData></Event>'
)
function Get-WinEvent {
    param($FilterHashtable, $MaxEvents, $ErrorAction)
    for ($index = 0; $index -lt $eventFixtureXML.Count; $index++) {
        $record = [pscustomobject]@{ Id = 1026 + $index; TimeCreated = [datetime]::UtcNow; Payload = $eventFixtureXML[$index] }
        $record | Add-Member ScriptMethod ToXml { return $this.Payload }
        $record
    }
}
$eventOutput = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
try {
    Export-TestEvents ([datetime]::UtcNow.AddMinutes(-1)) $eventOutput
    $serialized = Get-Content -Raw -LiteralPath $eventOutput
    foreach ($privateValue in @('private.example', 'sensitive message', '123456', '0xdeadbeef')) {
        Assert-True (-not $serialized.Contains($privateValue)) 'redacted Windows event data and rendering metadata'
    }
    $saved = ConvertFrom-Json -AsHashtable $serialized
    Assert-True ($saved.Count -eq 2 * $eventFixtureXML.Count) 'all records from both Windows logs retained'
    foreach ($offset in @(0, $eventFixtureXML.Count)) {
        Assert-True ($saved[$offset].codes.DisconnectReason -eq '4868' -and $saved[$offset].codes.ErrorCode -eq '0x0') 'named EventData reason retained'
        Assert-True ($saved[$offset + 1].codes.Reason -eq '4868' -and $saved[$offset + 1].codes.ErrorCode -eq '0x0') 'namespaced UserData reason retained'
        Assert-True ($saved[$offset + 2].codes.Count -eq 0) 'unnamed Data is not guessed'
        Assert-True ($saved[$offset + 3].codes.Count -eq 0) 'missing event payload does not abort export'
        Assert-True ($saved[$offset + 4].outcome -eq 'record_unavailable') 'malformed record marked independently'
        Assert-True ($saved[$offset + 5].codes.reason -eq '3334' -and $saved[$offset + 5].codes.errorCode -eq '0x80004005') 'later nested EventData with lowercase labels retained'
    }
} finally {
    Remove-Item Function:Get-WinEvent
    Remove-Item -LiteralPath $eventOutput -ErrorAction SilentlyContinue
}

if ($IsWindows) {
    & (Join-Path $PSScriptRoot 'Build.ps1')
    & (Join-Path $env:RUNNER_TEMP 'rdp-client.exe') --self-test
    if ($LASTEXITCODE -ne 0) { throw 'Client parser/pixel self-tests failed.' }
    & (Join-Path $env:RUNNER_TEMP 'rdp-client.exe') --check-control
    if ($LASTEXITCODE -ne 0) { throw 'Microsoft RDP ActiveX component preflight failed.' }
}
Write-Host 'RDP Windows orchestration tests passed.'
