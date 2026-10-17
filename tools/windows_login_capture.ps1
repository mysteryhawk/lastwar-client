[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Start', 'Stop', 'Status')]
    [string]$Action
)

$ErrorActionPreference = 'Stop'
$captureRoot = Join-Path $env:LOCALAPPDATA 'DoAC\private-captures'
$currentPath = Join-Path $captureRoot 'current-capture.txt'

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run this script from an Administrator PowerShell session.'
    }
}

function Invoke-Pktmon {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)
    $output = & pktmon @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "pktmon $($Arguments[0]) failed with exit code ${LASTEXITCODE}: $($output | Out-String)"
    }
    return $output
}

function Get-PktmonStatus {
    ((Invoke-Pktmon -Arguments @('status')) | Out-String).Trim()
}

Assert-Administrator

switch ($Action) {
    'Status' {
        Write-Output (Get-PktmonStatus)
        if (Test-Path -LiteralPath $currentPath) {
            Write-Output "Current private capture: $(Get-Content -Raw -LiteralPath $currentPath)"
        }
    }

    'Start' {
        $status = Get-PktmonStatus
        if ($status -match '(?im)^\s*Status:\s*Running\s*$') {
            throw 'Packet Monitor is already running; refusing to replace another capture.'
        }

        New-Item -ItemType Directory -Path $captureRoot -Force | Out-Null
        $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
        $etlPath = Join-Path $captureRoot "lastwar-login-$stamp.etl"

        Invoke-Pktmon -Arguments @('filter', 'remove') | Out-Null
        Invoke-Pktmon -Arguments @('filter', 'add', 'DoAC-role-1723', '-t', 'TCP', '-p', '11723') | Out-Null
        Invoke-Pktmon -Arguments @('filter', 'add', 'DoAC-initial-160', '-t', 'TCP', '-p', '10160') | Out-Null
        Invoke-Pktmon -Arguments @('start', '--capture', '--comp', 'nics', '--type', 'flow', '--pkt-size', '0', '--file-size', '64', '--log-mode', 'circular', '--file-name', $etlPath) | Out-Null
        # Windows PowerShell 5.1 does not recognise the newer utf8NoBOM name.
        Set-Content -LiteralPath $currentPath -Value $etlPath -Encoding utf8

        Write-Output 'Private Last War Login capture started.'
        Write-Output 'Cold-start Last War, wait for the base screen to finish loading, then run this script with -Action Stop.'
    }

    'Stop' {
        if (-not (Test-Path -LiteralPath $currentPath)) {
            throw 'No current DoAC capture marker exists.'
        }
        $etlPath = (Get-Content -Raw -LiteralPath $currentPath).Trim()
        if (-not $etlPath.StartsWith($captureRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Refusing a capture path outside the private capture directory.'
        }

        Invoke-Pktmon -Arguments @('stop') | Out-Null
        $pcapPath = [IO.Path]::ChangeExtension($etlPath, '.pcapng')
        Invoke-Pktmon -Arguments @('etl2pcap', $etlPath, '--out', $pcapPath) | Out-Null
        Invoke-Pktmon -Arguments @('filter', 'remove') | Out-Null
        Remove-Item -LiteralPath $currentPath

        Write-Output "Private capture saved: $pcapPath"
        Write-Output 'Do not upload, commit or paste this file; it contains live account traffic.'
    }
}
