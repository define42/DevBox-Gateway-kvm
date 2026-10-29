# Unit tests use an in-memory certificate collection, never a system trust store.
& {
    . (Join-Path $PSScriptRoot 'CertificateTrust.ps1')

    function New-PublicTestCertificate {
        $key = [Security.Cryptography.RSA]::Create(2048)
        $signed = $null
        try {
            $request = [Security.Cryptography.X509Certificates.CertificateRequest]::new(
                'CN=localhost', $key, [Security.Cryptography.HashAlgorithmName]::SHA256,
                [Security.Cryptography.RSASignaturePadding]::Pkcs1)
            $signed = $request.CreateSelfSigned([datetimeoffset]::UtcNow.AddMinutes(-1), [datetimeoffset]::UtcNow.AddHours(1))
            return [Security.Cryptography.X509Certificates.X509Certificate2]::new($signed.RawData)
        } finally {
            if ($null -ne $signed) { $signed.Dispose() }
            $key.Dispose()
        }
    }

    function New-MemoryTrustStore([string] $ReceiptPath) {
        $memoryStore = [pscustomobject]@{
            Certificates = [Security.Cryptography.X509Certificates.X509Certificate2Collection]::new()
            ReceiptPath = $ReceiptPath
            AddObservedReceipt = $false
            FailAfterAdd = $false
            IgnoreRemove = $false
            AddCount = 0
            RemoveCount = 0
        }
        $memoryStore | Add-Member -MemberType ScriptMethod -Name Add -Value {
            param($Certificate)
            $this.AddCount++
            $receipt = [IO.File]::ReadAllText($this.ReceiptPath) | ConvertFrom-Json -AsHashtable
            Assert-True ($receipt.thumbprint -ceq $Certificate.Thumbprint) 'ownership thumbprint persisted before Add'
            Assert-True ($receipt.certificate -ceq [Convert]::ToBase64String($Certificate.RawData)) 'ownership DER persisted before Add'
            $this.AddObservedReceipt = $true
            [void]$this.Certificates.Add($Certificate)
            if ($this.FailAfterAdd) { throw 'simulated_failure_after_add' }
        }
        $memoryStore | Add-Member -MemberType ScriptMethod -Name Remove -Value {
            param($Certificate)
            $this.RemoveCount++
            if (-not $this.IgnoreRemove) { $this.Certificates.Remove($Certificate) }
        }
        return $memoryStore
    }

    $directory = Join-Path ([IO.Path]::GetTempPath()) ('rdp-trust-tests-' + [guid]::NewGuid().ToString('N'))
    [void][IO.Directory]::CreateDirectory($directory)
    $receiptPath = Join-Path $directory 'trust-receipt.json'
    $fixture = $null
    $unrelated = $null
    $previousOptIn = $env:RDP_TEST_ALLOW_MACHINE_TRUST
    try {
        $env:RDP_TEST_ALLOW_MACHINE_TRUST = $null
        Assert-Rejected { Assert-FixtureTrustEnvironment } 'machine trust requires explicit opt-in'
        $env:RDP_TEST_ALLOW_MACHINE_TRUST = $previousOptIn
        $fixture = New-PublicTestCertificate
        $unrelated = New-PublicTestCertificate

        $store = New-MemoryTrustStore $receiptPath
        [void]$store.Certificates.Add($unrelated)
        Install-FixtureTrust $store $fixture $receiptPath
        Assert-True $store.AddObservedReceipt 'Add observed complete ownership journal'
        Assert-True ($store.Certificates.Count -eq 2) 'installation preserves unrelated certificate'
        Remove-FixtureTrust $store $fixture $receiptPath
        Assert-True ($store.Certificates.Count -eq 1 -and $store.Certificates[0].Thumbprint -ceq $unrelated.Thumbprint) 'cleanup removes only owned certificate'
        Assert-True ($store.RemoveCount -eq 1) 'cleanup removes exact certificate once'
        Remove-FixtureTrust $store $fixture $receiptPath
        Assert-True ($store.RemoveCount -eq 1) 'cleanup is idempotent after removal'
        [IO.File]::Delete($receiptPath)

        $store = New-MemoryTrustStore $receiptPath
        [void]$store.Certificates.Add($fixture)
        Assert-Rejected { Install-FixtureTrust $store $fixture $receiptPath } 'preexisting certificate refused'
        Assert-True (-not [IO.File]::Exists($receiptPath) -and $store.AddCount -eq 0) 'preexisting certificate creates no ownership receipt'
        Remove-FixtureTrust $store $fixture $receiptPath
        Assert-True ($store.Certificates.Count -eq 1 -and $store.RemoveCount -eq 0) 'no receipt means preexisting certificate is untouched'

        $store = New-MemoryTrustStore $receiptPath
        $store.FailAfterAdd = $true
        Assert-Rejected { Install-FixtureTrust $store $fixture $receiptPath } 'failure after insertion is propagated'
        Assert-True ($store.Certificates.Count -eq 1 -and [IO.File]::Exists($receiptPath)) 'partial install retains ownership evidence'
        Remove-FixtureTrust $store $fixture $receiptPath
        Assert-True ($store.Certificates.Count -eq 0) 'cleanup succeeds without install success result'
        [IO.File]::Delete($receiptPath)

        foreach ($field in @('thumbprint', 'certificate')) {
            $store = New-MemoryTrustStore $receiptPath
            Install-FixtureTrust $store $fixture $receiptPath
            $receipt = [IO.File]::ReadAllText($receiptPath) | ConvertFrom-Json -AsHashtable
            $receipt[$field] = 'mismatched'
            [IO.File]::WriteAllText($receiptPath, ($receipt | ConvertTo-Json -Compress))
            Assert-Rejected { Remove-FixtureTrust $store $fixture $receiptPath } ('mismatched receipt ' + $field + ' rejected')
            Assert-True ($store.Certificates.Count -eq 1 -and $store.RemoveCount -eq 0) 'receipt mismatch cannot remove certificate'
            [IO.File]::Delete($receiptPath)
        }

        $store = New-MemoryTrustStore $receiptPath
        Install-FixtureTrust $store $fixture $receiptPath
        $store.IgnoreRemove = $true
        Assert-Rejected { Remove-FixtureTrust $store $fixture $receiptPath } 'ineffective removal is reported as failure'
        Assert-True ($store.Certificates.Count -eq 1 -and [IO.File]::Exists($receiptPath)) 'failed cleanup retains ownership evidence'
    } finally {
        $env:RDP_TEST_ALLOW_MACHINE_TRUST = $previousOptIn
        if ($null -ne $fixture) { $fixture.Dispose() }
        if ($null -ne $unrelated) { $unrelated.Dispose() }
        [IO.File]::Delete($receiptPath)
        [IO.Directory]::Delete($directory)
    }
}
