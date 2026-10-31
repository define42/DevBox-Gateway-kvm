# Build with the Windows-included .NET Framework toolchain; no downloaded COM
# wrappers or packages. The executable stays outside the artifact directory.
$ErrorActionPreference = 'Stop'
$compiler = Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/csc.exe'
if (-not (Test-Path $compiler)) { throw 'The .NET Framework x64 compiler is unavailable.' }
$destination = Join-Path $env:RUNNER_TEMP 'rdp-client.exe'
& $compiler /nologo /target:exe /platform:x64 /warnaserror+ /out:$destination `
    /reference:System.Windows.Forms.dll /reference:System.Drawing.dll `
    /reference:Microsoft.CSharp.dll /reference:System.Web.Extensions.dll `
    (Join-Path $PSScriptRoot 'Client.cs')
if ($LASTEXITCODE -ne 0) { throw 'Microsoft RDP harness compilation failed.' }

# A separate entry point observes an actual mstsc.exe process. Client.cs supplies
# fixture pixel checks and reporting; this runner never hosts an ActiveX control.
$destination = Join-Path $env:RUNNER_TEMP 'mstsc-client.exe'
$wpf = Join-Path ([IO.Path]::GetDirectoryName($compiler)) 'WPF'
& $compiler /nologo /target:exe /platform:x64 /warnaserror+ /out:$destination `
    /main:DevBox.RdpSmoke.MstscProgram /lib:$wpf `
    /reference:System.Windows.Forms.dll /reference:System.Drawing.dll `
    /reference:Microsoft.CSharp.dll /reference:System.Web.Extensions.dll `
    /reference:UIAutomationClient.dll /reference:UIAutomationTypes.dll /reference:WindowsBase.dll `
    (Join-Path $PSScriptRoot 'Client.cs') (Join-Path $PSScriptRoot 'MstscClient.cs')
if ($LASTEXITCODE -ne 0) { throw 'mstsc.exe harness compilation failed.' }
