$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed, read-only VM inspection entry point. No caller arguments or caller-provided
# commands/paths are accepted. The ignored manifest selects one operation whose remote
# command is hard-coded below.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }

$root = 'C:\Users\edwar\Projects\DoAC\lastwar-client'
$manifestPath = Join-Path $root '.codex-transfer\vm-inspect.json'
$keyPath = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHostsPath = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'

if (!(Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'inspection manifest is missing' }
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if (@($manifest.PSObject.Properties.Name).Count -ne 1 -or
    @($manifest.PSObject.Properties.Name | Where-Object { $_ -ne 'operation' }).Count) {
    throw 'manifest field must be exactly operation'
}

$commands = @{
    resources = 'uptime; free -h; df -h /home/opc'
    lua_season_index = "grep -Ei 'season.*trend|season.*contribution|devotes|camp\.battle|conquest|explor|kill.*rank' /home/opc/lastwar-lua/matches.tsv /home/opc/lastwar-lua/modules.tsv | head -240"
    lua_layout = "find /home/opc/lastwar-lua -maxdepth 2 -type d -print | sort | head -100; find /home/opc/lastwar-lua -maxdepth 3 -type f \( -name '00401*' -o -name '01748*' -o -name '02786*' -o -name '02963*' \) -print | head -80"
    dashboard_status = 'systemctl --user is-active doac-dashboard.service; systemctl --user is-active doac-dashboard-tunnel.service; systemctl --user --no-pager --full status doac-dashboard.service doac-dashboard-tunnel.service | head -160'
    vs_status = @'
systemctl --user list-timers --all --no-pager | grep -E 'doac-(thp|base|vs)' || true; systemctl --user is-active doac-thp-mvp.service doac-base-level.service doac-thp-benchmark.service 2>/dev/null || true; python3 -c "import sqlite3; db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True); [print('|'.join(str(x) for x in row)) for row in db.execute(\"select metric,status,count(*),max(collected_at) from analytics_snapshots where metric in ('vs_daily','vs_weekly') group by metric,status order by metric,status\")]"
'@
    tunnel_url = "grep -Eo 'https://[A-Za-z0-9.-]+\.trycloudflare\.com' /home/opc/doac-private/dashboard-tunnel.log | tail -1"
}
$operation = [string]$manifest.operation
if (!$commands.ContainsKey($operation)) { throw 'unsupported read-only inspection operation' }

foreach ($required in @($keyPath, $knownHostsPath, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

& $ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- 'opc@140.238.71.236' $commands[$operation]
if ($LASTEXITCODE -ne 0) { throw "read-only VM inspection failed with exit code $LASTEXITCODE" }
