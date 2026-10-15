$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Intentionally accepts no arguments. It can only commit explicitly listed safe project
# files to, push, or replace draft PR #1's body for one of two fixed feature branches.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$controlRoot = 'C:\Users\edwar\Projects\DoAC\lastwar-client\.codex-transfer'
$manifestPath = Join-Path $controlRoot 'publish.json'
$projects = @{
    client = @{ Root='C:\Users\edwar\Projects\DoAC\lastwar-client'; Branch='feature/interactive-continuous-reader'; Repo='mysteryhawk/lastwar-client'; Remote='https://github.com/mysteryhawk/lastwar-client.git' }
    dashboard = @{ Root='C:\Users\edwar\Projects\DoAC\lastwar-dashboard'; Branch='feature/history-dashboard'; Repo='mysteryhawk/lastwar-dashboard'; Remote='https://github.com/mysteryhawk/lastwar-dashboard.git' }
}
if (!(Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'publish manifest is missing' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
$project = [string]$manifest.project
$action = [string]$manifest.action
if (!$projects.ContainsKey($project)) { throw 'project must be client or dashboard' }
if ($action -notin @('commit_push','push','update_pr')) { throw 'unsupported publish action' }
$expectedFields = if ($action -eq 'commit_push') { @('project','action','message','paths') } else { @('project','action') }
if (@($manifest.PSObject.Properties.Name).Count -ne $expectedFields.Count -or
    @($manifest.PSObject.Properties.Name | Where-Object { $_ -notin $expectedFields }).Count) {
    throw "manifest fields do not match action $action"
}
$spec = $projects[$project]
$actualRemote = (& git -C $spec.Root remote get-url origin).Trim()
$actualBranch = (& git -C $spec.Root branch --show-current).Trim()
if ($LASTEXITCODE -ne 0 -or $actualRemote -ne $spec.Remote -or $actualBranch -ne $spec.Branch) {
    throw 'repository remote or branch does not match the fixed policy'
}
if ($action -eq 'commit_push') {
    $message = [string]$manifest.message
    if ($message.Length -lt 3 -or $message.Length -gt 120 -or $message -match "[\r\n]") {
        throw 'commit message must be one line of 3..120 characters'
    }
    $paths = @($manifest.paths)
    if ($paths.Count -lt 1 -or $paths.Count -gt 100) { throw 'paths must contain 1..100 entries' }
    $root = [IO.Path]::GetFullPath($spec.Root).TrimEnd('\')
    $normalPaths = [Collections.Generic.List[string]]::new()
    foreach ($entry in $paths) {
        if ($entry -isnot [string] -or [string]::IsNullOrWhiteSpace($entry) -or [IO.Path]::IsPathRooted($entry)) {
            throw 'every path must be a non-empty relative string'
        }
        $full = [IO.Path]::GetFullPath((Join-Path $root $entry))
        if (!$full.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase) -or
            !(Test-Path -LiteralPath $full -PathType Leaf)) { throw "path is not an existing project file: $entry" }
        $normal = $entry.Replace('\','/')
        if ($normal -match '(?i)(^|/)(\.git|doac-private|captures?|secrets?|credentials?|sessions?|auth)(/|$)' -or
            $normal -match '(?i)(^|/)\.env($|\.)' -or
            $normal -match '(?i)\.(key|pem|p12|pfx|sqlite|sqlite3|db|jsonl|pcap|pcapng|log|env)$') {
            throw "sensitive file pattern is forbidden: $entry"
        }
        $normalPaths.Add($normal)
    }
    $alreadyStaged = @(& git -C $root diff --cached --name-only)
    $outside = @($alreadyStaged | Where-Object { $_ -notin $normalPaths })
    if ($outside.Count) { throw 'staged files outside the manifest are present' }
    & git -C $root add -- @($normalPaths)
    if ($LASTEXITCODE -ne 0) { throw 'git add failed' }
    $staged = @(& git -C $root diff --cached --name-only)
    if (!$staged.Count) { throw 'no listed changes are staged' }
    $outside = @($staged | Where-Object { $_ -notin $normalPaths })
    if ($outside.Count) { throw 'staged files outside the manifest are present after add' }
    & git -C $root commit -m $message --only -- @($normalPaths)
    if ($LASTEXITCODE -ne 0) { throw 'git commit failed' }
    & git -C $root push --porcelain origin ($spec.Branch + ':' + $spec.Branch)
    if ($LASTEXITCODE -ne 0) { throw "git push failed with exit code $LASTEXITCODE" }
    exit 0
}
if ($action -eq 'push') {
    & git -C $spec.Root push --porcelain origin ($spec.Branch + ':' + $spec.Branch)
    if ($LASTEXITCODE -ne 0) { throw "git push failed with exit code $LASTEXITCODE" }
    exit 0
}
$bodyPath = Join-Path $controlRoot ($project + '-pr-body.md')
if (!(Test-Path -LiteralPath $bodyPath -PathType Leaf)) { throw 'fixed PR body file is missing' }
$body = Get-Item -LiteralPath $bodyPath
if ($body.Length -lt 1 -or $body.Length -gt 32768 -or
    ($body.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'invalid PR body file' }
$pr = & gh pr view 1 --repo $spec.Repo --json headRefName,isDraft | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $pr.headRefName -ne $spec.Branch -or $pr.isDraft -ne $true) {
    throw 'PR #1 is not the expected draft on the fixed feature branch'
}
& gh pr edit 1 --repo $spec.Repo --body-file $bodyPath
if ($LASTEXITCODE -ne 0) { throw "PR update failed with exit code $LASTEXITCODE" }
