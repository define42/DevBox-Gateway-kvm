# No network calls on import. Functions are also exercised by Test.ps1.
Set-StrictMode -Version Latest

function Get-TestOrigin([string] $Value) {
    $uri = $null
    if (-not [Uri]::TryCreate($Value, [UriKind]::Absolute, [ref]$uri) -or
        $uri.Scheme -ne 'https' -or $uri.UserInfo -ne '' -or
        $uri.AbsolutePath -ne '/' -or $uri.Query -ne '' -or $uri.Fragment -ne '') {
        throw 'invalid_test_origin'
    }
    return $uri
}

function Get-TestCSRF([string] $HTML) {
    # Match the gateway's hidden field, not arbitrary markup or an error page.
    $match = [regex]::Match($HTML, '<input\s+type="hidden"\s+name="csrf"\s+value="([a-f0-9]{64})"\s*>')
    if (-not $match.Success) { throw 'missing_csrf' }
    return $match.Groups[1].Value
}

function Assert-TestProfile([string] $Profile, [Uri] $Origin, [string] $Username) {
    $settings = @{}
    foreach ($line in ($Profile -split '\r?\n')) {
        if ($line -eq '') { continue }
        $match = [regex]::Match($line, '^([^:\r\n]+):([is]):([^\r\n]*)$')
        if (-not $match.Success -or $settings.ContainsKey($match.Groups[1].Value)) {
            throw 'invalid_rdp_profile'
        }
        $settings[$match.Groups[1].Value] = $match.Groups[2].Value + ':' + $match.Groups[3].Value
    }
    # The gateway multiplexes HTTPS and RDP on one public address. Never send
    # the supplied password to a different address advertised by a profile.
    $hostPart = $Origin.DnsSafeHost
    if ($hostPart.Contains(':')) { $hostPart = '[' + $hostPart.Trim('[', ']') + ']' }
    $expected = $hostPart + ':' + $Origin.Port
    if ($settings['full address'] -ne ('s:' + $expected) -and
        -not ($Origin.Port -eq 3389 -and $settings['full address'] -eq ('s:' + $hostPart))) {
        throw 'rdp_endpoint_mismatch'
    }
    if ($settings['username'] -cne ('s:' + $Username) -or
        $settings['loadbalanceinfo'] -cnotmatch '^s:[a-f0-9]{64}$' -or
        $settings['authentication level'] -ne 'i:2' -or
        $settings['enablecredsspsupport'] -ne 'i:1' -or
        $settings['negotiate security layer'] -ne 'i:1' -or
        $settings['autoreconnection enabled'] -ne 'i:0') {
        throw 'unexpected_rdp_profile'
    }
}

function Invoke-TestHTTP($Client, [Uri] $Origin, [string] $Path, [hashtable] $Form) {
    $request = [System.Net.Http.HttpRequestMessage]::new()
    try {
        $request.RequestUri = [Uri]::new($Origin, $Path)
        if ($null -ne $Form) {
            $request.Method = [System.Net.Http.HttpMethod]::Post
            $pairs = [System.Collections.Generic.Dictionary[string,string]]::new()
            foreach ($key in $Form.Keys) { $pairs.Add($key, $Form[$key]) }
            $request.Content = [System.Net.Http.FormUrlEncodedContent]::new($pairs)
            $request.Headers.Add('Origin', $Origin.GetLeftPart([UriPartial]::Authority))
        }
        $response = $Client.SendAsync($request).GetAwaiter().GetResult()
        try {
            # Deliberately do not emit the response object or raw errors: HTML
            # may contain CSRF tokens and the RDP body contains a routing grant.
            return @{
                Status = [int]$response.StatusCode
                Body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                ContentType = [string]$response.Content.Headers.ContentType
                Location = [string]$response.Headers.Location
            }
        } finally { $response.Dispose() }
    } finally { $request.Dispose() }
}

function Get-TestReport($Client, [Uri] $Origin, [string] $VM) {
    $response = Invoke-TestHTTP $Client $Origin ('/perf?vm=' + $VM) $null
    if ($response.Status -ne 200 -or $response.ContentType -notlike 'application/json*') {
        throw 'diagnostics_unavailable'
    }
    $report = ConvertFrom-Json -InputObject $response.Body -AsHashtable
    if ($report.scope -ne 'current-user' -or $report.vms.Count -ne 1 -or $report.vms[0].id -ne $VM) {
        throw 'unexpected_diagnostics_scope'
    }
    return $report
}

function Test-ActiveEncoder($Report, [string] $Mode, [datetime] $Since) {
    $performance = $Report.vms[0].performance
    $expected = if ($Mode -eq 'bitmap') { 'bitmap' } else { 'software' }
    # Require a new heartbeat during this run, not a cached successful session.
    return ($performance.state -eq 'live' -and $performance.connections -eq 1 -and
        $performance.encoder -eq $expected -and $performance.framesSent -gt 0 -and
        [datetime]$performance.sampledAt -ge $Since)
}

function Export-TestEvents([datetime] $Since, [string] $OutputPath) {
    $events = @()
    foreach ($log in @('Microsoft-Windows-TerminalServices-RDPClient/Operational',
                      'Microsoft-Windows-TerminalServices-ClientActiveXCore/Operational')) {
        try {
            $records = Get-WinEvent -FilterHashtable @{ LogName = $log; StartTime = $Since } -MaxEvents 100 -ErrorAction Stop
        } catch {
            $events += @{ log = $log; outcome = 'unavailable_or_empty' }
            continue
        }
        foreach ($record in $records) {
            try {
                $numeric = @{}
                $xml = [xml]$record.ToXml()
                # Windows events may use named EventData fields or a provider's
                # namespaced UserData tree. DOM traversal avoids PowerShell's
                # XML property adaptation (a text-only Data can become a string)
                # and does not assume that EventData exists under StrictMode.
                $fields = $xml.SelectNodes("/*[local-name()='Event']/*[local-name()='EventData' or local-name()='UserData']//*[not(*)]")
                foreach ($field in $fields) {
                    $name = if ($field.LocalName -eq 'Data') { $field.GetAttribute('Name') } else { $field.LocalName }
                    $value = $field.InnerText.Trim()
                    # Export only numeric reason/error fields, never formatted
                    # messages, addresses, usernames, or arbitrary event data.
                    # Unnamed Data values cannot safely be assigned a meaning.
                    if ($name -match '(?i)(reason|error|code)' -and
                        $value -match '^(0x[0-9a-fA-F]+|-?[0-9]+)$') {
                        $numeric[$name] = $value
                    }
                }
                $events += @{ log = $log; id = $record.Id; timestamp = $record.TimeCreated.ToUniversalTime().ToString('o'); codes = $numeric }
            } catch {
                # One malformed or unavailable record must not discard later
                # records from the same log or masquerade as a missing channel.
                $events += @{ log = $log; outcome = 'record_unavailable' }
            }
        }
    }
    ConvertTo-Json -InputObject @($events) -Depth 6 | Set-Content -LiteralPath $OutputPath -Encoding utf8
}
