$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Single fixed entry point for the persistent Codex permission. It accepts no
# arguments and dispatches only to constrained helpers in this directory.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$root = 'C:\Users\edwar\Projects\DoAC\lastwar-client'
$manifestPath = Join-Path $root '.codex-transfer\operation.json'
if (!(Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'operation manifest is missing' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if (@($manifest.PSObject.Properties.Name).Count -ne 1 -or
    @($manifest.PSObject.Properties.Name | Where-Object { $_ -ne 'action' }).Count) {
    throw 'manifest field must be exactly action'
}
switch ([string]$manifest.action) {
    'transfer' { & (Join-Path $root 'tools\codex_vm_transfer.ps1') }
    'publish' { & (Join-Path $root 'tools\codex_publish.ps1') }
    'inspect' { & (Join-Path $root 'tools\codex_vm_inspect.ps1') }
    default { throw 'action must be transfer, publish or inspect' }
}
