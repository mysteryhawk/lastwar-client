$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Intentionally accepts no arguments. It can only push one fixed feature branch or
# replace draft PR #1's body for one of the two known repositories.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$controlRoot = 'C:\Users\edwar\Projects\DoAC\lastwar-client\.codex-transfer'
$manifestPath = Join-Path $controlRoot 'publish.json'
$projects = @{
    client = @{ Root='C:\Users\edwar\Projects\DoAC\lastwar-client'; Branch='feature/interactive-continuous-reader'; Repo='mysteryhawk/lastwar-client'; Remote='https://github.com/mysteryhawk/lastwar-client.git' }
    dashboard = @{ Root='C:\Users\edwar\Projects\DoAC\lastwar-dashboard'; Branch='feature/history-dashboard'; Repo='mysteryhawk/lastwar-dashboard'; Remote='https://github.com/mysteryhawk/lastwar-dashboard.git' }
}
if (!(Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'publish manifest is missing' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if (@($manifest.PSObject.Properties.Name).Count -ne 2 -or
    @($manifest.PSObject.Properties.Name | Where-Object { $_ -notin @('project','action') }).Count) {
    throw 'manifest fields must be exactly project and action'
}
$project = [string]$manifest.project
$action = [string]$manifest.action
if (!$projects.ContainsKey($project)) { throw 'project must be client or dashboard' }
if ($action -notin @('push','update_pr')) { throw 'action must be push or update_pr' }
$spec = $projects[$project]
$actualRemote = (& git -C $spec.Root remote get-url origin).Trim()
$actualBranch = (& git -C $spec.Root branch --show-current).Trim()
if ($LASTEXITCODE -ne 0 -or $actualRemote -ne $spec.Remote -or $actualBranch -ne $spec.Branch) {
    throw 'repository remote or branch does not match the fixed policy'
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
