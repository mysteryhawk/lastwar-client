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
    go_toolchain = 'command -v go || true; find /home/opc -maxdepth 5 -type f \( -name go -o -name gofmt \) -perm -u+x -print | sort | head -20'
    lua_season_index = "grep -Ei 'season.*trend|season.*contribution|devotes|camp\.battle|conquest|explor|kill.*rank' /home/opc/lastwar-lua/matches.tsv /home/opc/lastwar-lua/modules.tsv | head -240"
    lua_layout = "find /home/opc/lastwar-lua -maxdepth 2 -type d -print | sort | head -100; find /home/opc/lastwar-lua -maxdepth 3 -type f \( -name '00401*' -o -name '01748*' -o -name '02786*' -o -name '02963*' \) -print | head -80"
    lua_alliance_rank = "grep -Ei 'alliance.*(power|rank)|rank.*alliance|alliancename|allianceName|alliance.*abbr' /home/opc/lastwar-lua/matches.tsv /home/opc/lastwar-lua/modules.tsv | head -320"
    lua_alliance_rank_detail = @'
for n in 00457 00459 01044 04701 04702 04708 04709 05772 05773 05774; do f=$(find /home/opc/lastwar-lua -type f -name "$n.luac" | head -1); test -n "$f" || continue; echo FILE:$f; strings "$f" | head -320; done
'@
    lua_alliance_rank_command = "grep -aREi 'alliance[._].*rank|rank[._].*alliance|AllianceRankListMessage|ParseAllianceRankData' /home/opc/lastwar-lua/extracted | head -300"
    lua_alliance_rank_fields = @'
for n in 04701 04702 04708 04709 05772 05773 05774; do f=$(find /home/opc/lastwar-lua -type f -name "$n.luac" | head -1); test -n "$f" || continue; echo FILE:$f; strings "$f" | grep -Ei 'alliance|fightpower|member|count|num|leader|server|rank' | head -180; done
'@
    client_bundle_checks = @'
set -eu
test -s /tmp/doac-dev/incoming/lastwar-client-bundle.zip
d=$(mktemp -d /tmp/doac-dev/client-check.XXXXXX)
trap 'rm -rf "$d"' EXIT
git clone --quiet --depth 1 --branch feature/interactive-continuous-reader https://github.com/mysteryhawk/lastwar-client.git "$d"
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d"
if test -s "$d/.codex-transfer/artifacts/alliance-contract.zip"; then
  unzip -oq "$d/.codex-transfer/artifacts/alliance-contract.zip" -d "$d"
fi
cd "$d"
bad=$(/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/analytics/types.go internal/analytics/parse.go internal/analytics/analytics_test.go)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
'@
    dashboard_status = 'systemctl --user is-active doac-dashboard.service; systemctl --user is-active doac-dashboard-tunnel.service; systemctl --user --no-pager --full status doac-dashboard.service doac-dashboard-tunnel.service | head -160'
    vs_status = @'
systemctl --user list-timers --all --no-pager | grep -E 'doac-(thp|base|vs)' || true; systemctl --user is-active doac-thp-mvp.service doac-base-level.service doac-thp-benchmark.service 2>/dev/null || true; python3 -c "import sqlite3; db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True); [print('|'.join(str(x) for x in row)) for row in db.execute(\"select metric,status,count(*),max(collected_at) from analytics_snapshots where metric in ('vs_daily','vs_weekly') group by metric,status order by metric,status\")]"
'@
    tunnel_url = "grep -Eo 'https://[A-Za-z0-9.-]+\.trycloudflare\.com' /home/opc/doac-private/dashboard-tunnel.log | tail -1"
    alliance_deploy_state = @'
set -eu
uptime
free -h
df -h /home/opc
systemctl --user cat doac-dashboard.service
systemctl --user list-timers --all --no-pager | grep -E 'doac-(thp|base|vs|benchmark|alliance)' || true
readlink -f /home/opc/lastwar-client/lastwar-client
test -x /home/opc/lastwar-client/lastwar-client
test -f /home/opc/doac-private/doac-history.sqlite3
python3 -c "import sqlite3; db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True); print(db.execute('pragma user_version').fetchone()[0]); print([r[0] for r in db.execute(\"select name from sqlite_master where type='table' and name like 'alliance_ranking_%' order by name\")])"
'@
    alliance_bundle_list = "unzip -l /tmp/doac-dev/incoming/lastwar-client-bundle.zip | head -40"
    alliance_bundle_validate = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/bundle-check.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/packages"
find "$d/packages" -maxdepth 4 -type f -printf '%P %s\n' | sort
mkdir -p "$d/client" "$d/dashboard"
unzip -oq "$d/packages/.codex-transfer/artifacts/client-a1522ef.zip" -d "$d/client"
unzip -oq "$d/packages/.codex-transfer/artifacts/dashboard-9e67051.zip" -d "$d/dashboard"
test -f "$d/client/go.mod"
test -f "$d/client/internal/analytics/collector.go"
test -f "$d/dashboard/tools/run_alliance_power.py"
echo bundle-ok
'@
    alliance_bundle_gofmt = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/bundle-gofmt.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/packages"
mkdir -p "$d/client"
unzip -oq "$d/packages/.codex-transfer/artifacts/client-a1522ef.zip" -d "$d/client"
cd "$d/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -d internal/analytics/alliance.go internal/analytics/config.go
echo gofmt-check-complete
'@
    alliance_runner_state = @'
set -eu
systemctl --user cat doac-thp-mvp.service
systemctl --user cat doac-vs-mvp.service
find /home/opc/doac-private -maxdepth 1 -type f -printf '%f\n' | sort | grep -E '^(collector|alliance|session|config|doac-history)' || true
systemctl --user is-active doac-dashboard.service
systemctl --user is-active doac-dashboard-tunnel.service
'@
    alliance_failed_snapshot_summary = @'
set -eu
python3 - <<'PY'
import glob, json, os
paths=sorted(glob.glob('/home/opc/doac-private/captures/alliance-power-*.jsonl'))
if not paths:
    raise SystemExit('no retained failed alliance snapshot')
path=paths[-1]
with open(path,encoding='utf-8') as f:
    lines=f.read().splitlines()
if len(lines)!=1:
    raise SystemExit('unexpected snapshot line count')
doc=json.loads(lines[0])
with open('/home/opc/doac-private/alliance-id',encoding='utf-8') as f:
    tracked=f.read().strip()
rows=doc.get('alliances') or []
tracked_rows=[r for r in rows if str(r.get('alliance_id',''))==tracked]
doac=[r for r in rows if str(r.get('abbr','')).casefold()=='doac']
print({'file':os.path.basename(path),'status':doc.get('status'),
       'rows':len(rows),'rejected_rows':doc.get('rejected_rows'),
       'tracked_id_length':len(tracked),'tracked_id_matches':len(tracked_rows),
       'doac_matches':len(doac),
       'doac':[{'name':r.get('name'),'abbr':r.get('abbr'),'rank':r.get('rank'),
                'total_power':r.get('total_power'),'source_server':r.get('source_server'),
                'member_count':r.get('member_count')} for r in doac]})
PY
'@
    alliance_dashboard_read_model = @'
set -eu
cd /home/opc/doac-dashboard
/home/opc/doac-dashboard/.venv/bin/python - <<'PY'
from dashboard import history
db_path='/home/opc/doac-private/doac-history.sqlite3'
with history.connect(db_path) as db:
    data=history.opponent_scout(db,1723,'thp')
rows=[r for r in data['alliances'] if str(r.get('alliance_abbr','')).casefold()=='doac']
print({'ranking_snapshot':bool(data['alliance_ranking_snapshot']),
       'alliance_cards':len(data['alliances']),'doac_matches':len(rows),
       'doac':[{'name':r.get('alliance_name'),'abbr':r.get('alliance_abbr'),
                'total_power':r.get('total_power'),'member_count':r.get('member_count'),
                'member_count_verified':r.get('member_count_verified'),
                'top200_thp_count':r.get('top200_thp_count'),
                'top_five_count':len(r.get('top_five_thp') or [])} for r in rows]})
PY
curl -sS -o /dev/null -w 'dashboard_http=%{http_code}\n' http://127.0.0.1:8080/opponents?server=1723
'@
    doac_alliance_overview = @'
set -eu
cd /home/opc/doac-dashboard
/home/opc/doac-dashboard/.venv/bin/python - <<'PY'
from pathlib import Path
from dashboard import history
aid=Path('/home/opc/doac-private/alliance-id').read_text(encoding='utf-8').strip()
with history.connect('/home/opc/doac-private/doac-history.sqlite3') as db:
    d=history.doac_review_groups(db,1723,aid,80_000_000,7_200_000)
print({'name':d['alliance_name'],'member_count':d['member_count'],
       'member_count_verified':d['member_count_verified'],'total_power':d['total_power'],
       'average_power':d['average_power'],'top200_thp_count':d['top200_thp_count'],
       'top_five_count':len(d['top_five_thp']),'elapsed_vs_days':d['elapsed_days'],
       'groups':{k:len(v) for k,v in d['groups'].items()},
       'insufficient':len(d['insufficient'])})
PY
'@
    alliance_current_bundle_checks = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/alliance-check.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/packages"
mkdir -p "$d/client"
unzip -oq "$d/packages/.codex-transfer/artifacts/client-6d9c809.zip" -d "$d/client"
for path in internal/analytics/alliance.go internal/analytics/analytics_test.go; do
  install -m 0644 "$d/packages/$path" "$d/client/$path"
done
cd "$d/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/types.go
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
'@
    alliance_clean_export_checks = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/alliance-clean-export.XXXXXX)
trap 'rm -rf "$d"' EXIT
git clone --quiet --depth 1 --branch feature/interactive-continuous-reader https://github.com/mysteryhawk/lastwar-client.git "$d/client"
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/overlay"
install -m 0644 "$d/overlay/internal/analytics/alliance.go" "$d/client/internal/analytics/alliance.go"
install -m 0644 "$d/overlay/internal/analytics/analytics_test.go" "$d/client/internal/analytics/analytics_test.go"
cd "$d/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go
bad=$(/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/analytics/alliance.go internal/analytics/analytics_test.go)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
'@
    lua_decompiler_state = @'
set -eu
f=$(find /home/opc/lastwar-lua -type f -name '00457.luac' | head -1)
test -n "$f"
file "$f"
xxd -l 48 "$f"
for tool in luac luadec unluac java python3; do command -v "$tool" || true; done
find /home/opc/lastwar-lua /home/opc/.local -maxdepth 5 -type f \( -iname '*unluac*' -o -iname '*luadec*' -o -name 'luac' \) -print 2>/dev/null | head -40
if command -v luac >/dev/null 2>&1; then luac -l -l "$f" | head -260 || true; fi
'@
    lua_alliance_rank_decompile = "java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar /home/opc/lastwar-lua/fixed/00457.luac | head -360"
    lua_alliance_fixed = @'
set -eu
find /home/opc/lastwar-lua/fixed /home/opc/lastwar-lua/decompiled -maxdepth 3 -type f \( -name '00457*' -o -iname '*AllianceRankData*' \) -print
for f in $(find /home/opc/lastwar-lua/decompiled -maxdepth 3 -type f \( -name '00457*' -o -iname '*AllianceRankData*' \) | head -4); do echo FILE:$f; sed -n '1,320p' "$f"; done
'@
}
$operation = [string]$manifest.operation
if (!$commands.ContainsKey($operation)) { throw 'unsupported read-only inspection operation' }

foreach ($required in @($keyPath, $knownHostsPath, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

& $ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- 'opc@140.238.71.236' $commands[$operation]
if ($LASTEXITCODE -ne 0) { throw "read-only VM inspection failed with exit code $LASTEXITCODE" }
