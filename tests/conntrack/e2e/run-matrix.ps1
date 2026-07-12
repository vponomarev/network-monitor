[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateScript({ Test-Path -LiteralPath $_ -PathType Leaf })]
    [string]$BinaryPath,

    [string]$User = "root",
    [string[]]$Hosts = @("192.168.5.99", "192.168.5.193", "192.168.5.214", "192.168.5.217"),
    [string]$PuttyDirectory = "C:\Program Files\PuTTY",
    [PSCredential]$Credential,
    [switch]$KeepRemote
)

$ErrorActionPreference = "Stop"
$plink = Join-Path $PuttyDirectory "plink.exe"
$pscp = Join-Path $PuttyDirectory "pscp.exe"
$hostScript = Join-Path $PSScriptRoot "run-host.sh"

foreach ($path in @($plink, $pscp, $hostScript)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required file not found: $path"
    }
}

$hostKeys = @{
    "192.168.5.99"  = "ssh-ed25519 255 b3:70:94:2f:14:0e:8e:5e:65:d2:d3:26:a5:2e:53:85"
    "192.168.5.193" = "ssh-ed25519 255 5d:20:bb:11:01:fc:6c:22:4f:0c:d0:23:58:d9:6d:4b"
    "192.168.5.214" = "ssh-ed25519 255 83:05:f8:b7:df:38:04:90:c6:7e:64:be:ad:fb:ef:cc"
    "192.168.5.217" = "ssh-ed25519 255 17:75:16:b4:67:28:26:09:9a:3f:37:92:3d:24:76:79"
}

$credential = if ($Credential) {
    $Credential
} else {
    Get-Credential -UserName $User -Message "SSH credentials for conntrack E2E hosts"
}
$passwordPtr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($credential.Password)
try {
    $password = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPtr)
    $runId = [Guid]::NewGuid().ToString("N")
    $results = @()

    foreach ($hostName in $Hosts) {
        if (-not $hostKeys.ContainsKey($hostName)) {
            throw "No pinned SSH host key for $hostName"
        }

        $remoteDir = "/tmp/conntrack-e2e-$runId"
        $target = "$User@$hostName"
        Write-Host "=== $hostName ==="

        & $plink -batch -hostkey $hostKeys[$hostName] -pw $password $target "mkdir -p '$remoteDir'"
        if ($LASTEXITCODE -ne 0) { throw "Failed to prepare $hostName" }

        & $pscp -batch -hostkey $hostKeys[$hostName] -pw $password `
            (Resolve-Path -LiteralPath $BinaryPath).Path `
            (Resolve-Path -LiteralPath $hostScript).Path `
            "${target}:${remoteDir}/"
        if ($LASTEXITCODE -ne 0) { throw "Failed to upload E2E files to $hostName" }

        $remoteCommand = "chmod 0755 '$remoteDir/conntrack-linux-amd64' '$remoteDir/run-host.sh' && '$remoteDir/run-host.sh' '$remoteDir/conntrack-linux-amd64'"
        $output = & $plink -batch -hostkey $hostKeys[$hostName] -pw $password $target $remoteCommand 2>&1
        $exitCode = $LASTEXITCODE
        $output | Write-Host
        $results += [pscustomobject]@{ Host = $hostName; ExitCode = $exitCode; Output = ($output -join "`n") }

        if (-not $KeepRemote) {
            & $plink -batch -hostkey $hostKeys[$hostName] -pw $password $target "rm -rf '$remoteDir'" | Out-Null
        }

        if ($exitCode -ne 0) { break }
    }

    $results | Select-Object Host, ExitCode | Format-Table -AutoSize
    if ($results.Count -ne $Hosts.Count -or ($results | Where-Object ExitCode -ne 0)) {
        throw "Conntrack E2E matrix failed"
    }
}
finally {
    if ($passwordPtr -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPtr)
    }
    $password = $null
}
