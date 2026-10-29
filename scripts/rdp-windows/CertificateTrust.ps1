param(
    [ValidateSet('', 'Install', 'Remove')][string] $Action = '',
    [string] $StatePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-FixtureTrustEnvironment {
    # This is a machine-wide change, permitted only on disposable hosted runners.
    # Refuse instead of requesting elevation or falling back to an interactive store.
    if (-not $IsWindows -or $env:GITHUB_ACTIONS -cne 'true' -or
        $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or $env:RDP_TEST_ALLOW_MACHINE_TRUST -cne '1') {
        throw 'fixture_trust_not_authorized'
    }
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    try {
        $principal = [Security.Principal.WindowsPrincipal]::new($identity)
        if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
            throw 'fixture_trust_requires_administrator'
        }
    } finally { $identity.Dispose() }
}

function Install-FixtureTrust($Store, $Certificate, [string] $ReceiptPath) {
    if ($Certificate.HasPrivateKey -or $Certificate.Subject -cne 'CN=localhost' -or
        $Certificate.Subject -cne $Certificate.Issuer -or
        $Certificate.NotAfter.ToUniversalTime() -le [datetime]::UtcNow -or
        $Certificate.NotBefore.ToUniversalTime() -gt [datetime]::UtcNow -or
        ($Certificate.NotAfter - $Certificate.NotBefore).TotalHours -gt 2) {
        throw 'invalid_fixture_certificate'
    }
    if ($Store.Certificates.Find('FindByThumbprint', $Certificate.Thumbprint, $false).Count -ne 0) {
        throw 'fixture_certificate_already_present'
    }
    # Journal ownership BEFORE Add. The parent must clean up even if this child
    # times out after Add, before the success report is written. CreateNew also
    # refuses to overwrite an earlier install's ownership information.
    $receipt = @{
        thumbprint = $Certificate.Thumbprint
        certificate = [Convert]::ToBase64String($Certificate.RawData)
    } | ConvertTo-Json -Compress
    $file = [IO.File]::Open($ReceiptPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $bytes = [Text.Encoding]::UTF8.GetBytes($receipt)
        $file.Write($bytes, 0, $bytes.Length)
        $file.Flush($true)
    } finally { $file.Dispose() }
    $Store.Add($Certificate)
    if ($Store.Certificates.Find('FindByThumbprint', $Certificate.Thumbprint, $false).Count -ne 1) {
        throw 'fixture_certificate_install_failed'
    }
}

function Remove-FixtureTrust($Store, $Certificate, [string] $ReceiptPath) {
    # No receipt means no attempt to add our certificate; never remove a
    # certificate that the absence check rejected as already present.
    if (-not [IO.File]::Exists($ReceiptPath)) { return }
    $receipt = [IO.File]::ReadAllText($ReceiptPath) | ConvertFrom-Json -AsHashtable
    $der = [Convert]::ToBase64String($Certificate.RawData)
    if ($receipt.thumbprint -cne $Certificate.Thumbprint -or $receipt.certificate -cne $der) {
        throw 'fixture_certificate_receipt_mismatch'
    }
    $matches = $Store.Certificates.Find('FindByThumbprint', $Certificate.Thumbprint, $false)
    foreach ($item in $matches) {
        if ([Convert]::ToBase64String($item.RawData) -cne $der) { throw 'fixture_certificate_receipt_mismatch' }
    }
    foreach ($item in $matches) { $Store.Remove($item) }
    if ($Store.Certificates.Find('FindByThumbprint', $Certificate.Thumbprint, $false).Count -ne 0) {
        throw 'fixture_certificate_cleanup_failed'
    }
}

# Dot-sourcing without an action loads the testable helpers without changing trust.
if ($Action -ne '') {
    $store = $null
    $certificate = $null
    $result = @{ outcome = 'failed'; reason = 'fixture_certificate_operation_failed' }
    try {
        Assert-FixtureTrustEnvironment
        $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::new((Join-Path $StatePath 'server.cer'))
        # CurrentUser Root may show a security consent dialog even from pwsh
        # -NonInteractive. Elevated LocalMachine Root does not need that dialog.
        $store = [Security.Cryptography.X509Certificates.X509Store]::new('Root', 'LocalMachine')
        $store.Open([Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
        $receiptPath = Join-Path $StatePath 'trust-receipt.json'
        if ($Action -eq 'Install') { Install-FixtureTrust $store $certificate $receiptPath }
        else { Remove-FixtureTrust $store $certificate $receiptPath }
        $result = @{ outcome = 'passed'; reason = $null }
    } catch {
        if ($_.Exception.Message -cin @('fixture_trust_not_authorized', 'fixture_trust_requires_administrator',
            'invalid_fixture_certificate', 'fixture_certificate_already_present', 'fixture_certificate_install_failed',
            'fixture_certificate_receipt_mismatch', 'fixture_certificate_cleanup_failed')) {
            $result.reason = $_.Exception.Message
        }
    } finally {
        if ($null -ne $store) { $store.Dispose() }
        if ($null -ne $certificate) { $certificate.Dispose() }
    }
    $result | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $StatePath ('trust-' + $Action + '.json')) -Encoding utf8
    Write-Host ('Fixture certificate ' + $Action + ': ' + $result.outcome)
    if ($result.outcome -ne 'passed') { exit 1 }
}
