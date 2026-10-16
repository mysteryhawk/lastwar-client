$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Intentionally accepts no arguments. A narrowly matched Codex exec-policy rule may
# invoke this script; all variable input comes from this ignored manifest and is
# validated below.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }

$clientRoot = 'C:\Users\edwar\Projects\DoAC\lastwar-client'
$dashboardRoot = 'C:\Users\edwar\Projects\DoAC\lastwar-dashboard'
$manifestPath = Join-Path $clientRoot '.codex-transfer\vm-transfer.json'
$keyPath = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHostsPath = 'C:\Users\edwar\.ssh\known_hosts'
$scp = 'C:\Windows\System32\OpenSSH\scp.exe'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
$roots = @{ client = $clientRoot; dashboard = $dashboardRoot }
$remoteNames = @{ client = 'lastwar-client-bundle.zip'; dashboard = 'lastwar-dashboard-bundle.zip' }

if (!(Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'transfer manifest is missing' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if (@($manifest.PSObject.Properties.Name).Count -ne 2 -or
    @($manifest.PSObject.Properties.Name | Where-Object { $_ -notin @('project','paths') }).Count) {
    throw 'manifest fields must be exactly project and paths'
}
$project = [string]$manifest.project
if (!$roots.ContainsKey($project)) { throw 'project must be client or dashboard' }
$paths = @($manifest.paths)
if ($paths.Count -lt 1 -or $paths.Count -gt 100) { throw 'paths must contain 1..100 entries' }

$root = [IO.Path]::GetFullPath($roots[$project]).TrimEnd('\')
$stagePrefix = (Join-Path $root '.codex-transfer\artifacts').TrimEnd('\') + '\'
$files = [Collections.Generic.List[object]]::new()
$total = [int64]0
foreach ($entry in $paths) {
    if ($entry -isnot [string] -or [string]::IsNullOrWhiteSpace($entry) -or [IO.Path]::IsPathRooted($entry)) {
        throw 'every path must be a non-empty relative string'
    }
    $full = [IO.Path]::GetFullPath((Join-Path $root $entry))
    if (!$full.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'path leaves project root' }
    if (!(Test-Path -LiteralPath $full -PathType Leaf)) { throw "source is not a file: $entry" }
    $item = Get-Item -Force -LiteralPath $full
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'reparse points are forbidden' }
    $normal = $entry.Replace('\','/')
    if ($normal -match '(?i)(^|/)(\.git|doac-private|captures?|secrets?|credentials?|sessions?|auth)(/|$)' -or
        $normal -match '(?i)(^|/)\.env($|\.)' -or
        $normal -match '(?i)\.(key|pem|p12|pfx|sqlite|sqlite3|db|jsonl|pcap|pcapng|log|env)$') {
        throw "sensitive file pattern is forbidden: $entry"
    }
    $isArtifact = $full.StartsWith($stagePrefix, [StringComparison]::OrdinalIgnoreCase)
    if (!$isArtifact) {
        & git -C $root ls-files --error-unmatch -- $normal *> $null
        if ($LASTEXITCODE -ne 0) { throw "source must be tracked or staged as an artefact: $entry" }
    }
    if ($item.Length -gt 64MB) { throw "individual file exceeds 64 MiB: $entry" }
    $total += $item.Length
    if ($total -gt 128MB) { throw 'bundle exceeds 128 MiB' }
    $files.Add([pscustomobject]@{ Full = $full; Relative = $normal })
}

if (!(Test-Path -LiteralPath $keyPath -PathType Leaf)) { throw 'configured SSH key is unavailable' }
if (!(Test-Path -LiteralPath $knownHostsPath -PathType Leaf)) { throw 'configured SSH known-hosts file is unavailable' }
if (!(Test-Path -LiteralPath $scp -PathType Leaf)) { throw 'system scp is unavailable' }
if (!(Test-Path -LiteralPath $ssh -PathType Leaf)) { throw 'system ssh is unavailable' }
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ('doac-transfer-' + [guid]::NewGuid().ToString('N'))
$contentRoot = Join-Path $tempRoot $project
$archive = Join-Path $tempRoot $remoteNames[$project]
try {
    New-Item -ItemType Directory -Path $contentRoot -Force | Out-Null
    foreach ($file in $files) {
        $destination = Join-Path $contentRoot $file.Relative
        New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
        Copy-Item -LiteralPath $file.Full -Destination $destination
    }
    # -Path with '*' silently omits the hidden .codex-transfer staging tree.
    # Enumerate with -Force so reviewed generated artefacts are actually sent.
    $archiveEntries = @(Get-ChildItem -Force -LiteralPath $contentRoot | Select-Object -ExpandProperty FullName)
    if ($archiveEntries.Count -eq 0) { throw 'bundle staging directory is empty' }
    Compress-Archive -LiteralPath $archiveEntries -DestinationPath $archive -CompressionLevel Optimal
    & $ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- 'opc@140.238.71.236' 'umask 077; mkdir -p /tmp/doac-dev/incoming; test -d /tmp/doac-dev/incoming'
    if ($LASTEXITCODE -ne 0) { throw "remote staging preparation failed with exit code $LASTEXITCODE" }
    & $scp -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- $archive 'opc@140.238.71.236:/tmp/doac-dev/incoming/'
    if ($LASTEXITCODE -ne 0) { throw "scp failed with exit code $LASTEXITCODE" }
    $remoteArchive = '/tmp/doac-dev/incoming/' + $remoteNames[$project]
    & $ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- 'opc@140.238.71.236' "test -s '$remoteArchive'"
    if ($LASTEXITCODE -ne 0) { throw "remote staging verification failed with exit code $LASTEXITCODE" }
} finally {
    if (Test-Path -LiteralPath $tempRoot) { Remove-Item -LiteralPath $tempRoot -Recurse -Force }
}
