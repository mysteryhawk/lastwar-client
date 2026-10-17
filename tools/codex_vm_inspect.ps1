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
    tunnel_public_health = @'
set -eu
url=$(grep -Eo 'https://[A-Za-z0-9.-]+\.trycloudflare\.com' /home/opc/doac-private/dashboard-tunnel.log | tail -1)
test -n "$url"
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "$url/login")
printf '%s\n%s\n' "$url" "$code"
test "$code" = 200
'@
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
       'alliance_rank':d.get('alliance_rank'),
       'member_count_verified':d['member_count_verified'],'total_power':d['total_power'],
       'average_power':d['average_power'],'top200_thp_count':d['top200_thp_count'],
       'top_five_count':len(d['top_five_thp']),'elapsed_vs_days':d['elapsed_days'],
       'groups':{k:len(v) for k,v in d['groups'].items()},
       'insufficient':len(d['insufficient'])})
PY
'@
    dashboard_alliance_rank_ui_checks = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/dashboard-rank-ui.XXXXXX)
trap 'rm -rf "$d"' EXIT
cp -a /home/opc/doac-dashboard/. "$d/dashboard"
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$d/overlay"
for path in dashboard/history.py dashboard/test_dashboard.py dashboard/templates/compare.html dashboard/templates/opponents.html; do
  install -m 0644 "$d/overlay/$path" "$d/dashboard/$path"
done
cd "$d/dashboard"
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s tools -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m unittest dashboard.test_dashboard
/home/opc/doac-dashboard/.venv/bin/python -m py_compile dashboard/history.py dashboard/test_dashboard.py
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
    lua_alliance_setrank_refs = @'
set -eu
grep -Ei 'AllianceRank|SetRank|selfRanking|allianceRanking' /home/opc/lastwar-lua/modules.tsv /home/opc/lastwar-lua/matches.tsv | head -320
find /home/opc/lastwar-lua/decompiled -type f -maxdepth 4 -print0 2>/dev/null | xargs -0 grep -HnE 'AllianceRankData|SetRank|selfRanking|allianceRanking' 2>/dev/null | head -320 || true
'@
    lua_rank_manager_alliance = "sed -n '65,150p' /home/opc/lastwar-lua/decompiled/managers/RankDataManager.lua"
    lua_alliance_rank_message_decompile = @'
set -eu
f=$(find /home/opc/lastwar-lua/fixed -type f -name '01044.luac' | head -1)
test -n "$f"
java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$f" | head -260
'@
    lua_fightpower_index = @'
set -eu
find /home/opc/lastwar-lua -maxdepth 2 -type f -printf '%P\n' | sort | head -160
grep -RIl --include='*.lua' -E 'fightpower|fightPower' /home/opc/lastwar-lua/decompiled 2>/dev/null | head -160 || true
'@
    lua_fightpower_bytecode = @'
set -eu
find /home/opc/lastwar-lua/fixed -type f -name '*.luac' -print0 | xargs -0 grep -aIl -E 'fightpower|fightPower' | head -240
'@
    lua_ismerge_refs = @'
set -eu
for f in $(find /home/opc/lastwar-lua/fixed -type f -name '*.luac'); do
  if strings "$f" | grep -q '^ismerge$'; then
    b=$(basename "$f")
    echo "FILE:$b"
    grep -F "$b" /home/opc/lastwar-lua/matches.tsv || true
    strings "$f" | grep -Ei 'alliance|rank|merge|server|fightpower' | head -80
  fi
done
'@
    lua_alliance_info_commands = @'
set -eu
grep -nEi 'Alliance.*(Info|Detail|Search|Rank|List)|(^|[[:space:]])Al.*Info' /home/opc/lastwar-lua/decompiled/MsgDefines.lua | head -320
'@
    lua_get_alliance_info_trace = @'
set -eu
grep -nE 'GetAllianceInfo|get\.al\.info' /home/opc/lastwar-lua/modules.tsv /home/opc/lastwar-lua/matches.tsv | head -160 || true
find /home/opc/lastwar-lua/decompiled -maxdepth 4 -type f -name '*.lua' -print0 2>/dev/null | xargs -0 grep -HnE 'GetAllianceInfo|get\.al\.info' 2>/dev/null | head -240 || true
for f in $(find /home/opc/lastwar-lua/fixed -type f -name '*.luac'); do
  if strings "$f" | grep -qE 'GetAllianceInfo|get\.al\.info'; then
    b=$(basename "$f")
    echo "FILE:$b"
    grep -F "$b" /home/opc/lastwar-lua/matches.tsv || true
    java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$f" 2>/dev/null | head -320 || true
  fi
done
'@
    lua_get_alliance_info_message = @'
set -eu
f=$(find /home/opc/lastwar-lua/fixed -type f -name '00661.luac' | head -1)
test -n "$f"
java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$f" | head -360
find /home/opc/lastwar-lua/decompiled -maxdepth 4 -type f -name '*.lua' -print0 2>/dev/null | xargs -0 grep -HnE 'TryShowAllianceInfo|GetAllianceInfo' 2>/dev/null | head -260 || true
'@
    lua_alliance_temp_manager = @'
set -eu
grep -nE 'AllianceTempListManager' /home/opc/lastwar-lua/modules.tsv /home/opc/lastwar-lua/matches.tsv | head -40
for f in $(grep -E 'AllianceTempListManager' /home/opc/lastwar-lua/matches.tsv | cut -f1 | head -4); do
  p=$(find /home/opc/lastwar-lua/fixed -type f -name "$f" | head -1)
  test -n "$p"
  echo "FILE:$f"
  java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$p" | head -520
done
'@
    lua_alliance_base_info = @'
set -eu
grep -nE 'AllianceBaseInfo\.luac' /home/opc/lastwar-lua/modules.tsv /home/opc/lastwar-lua/matches.tsv | head -40
for f in $(grep -E 'AllianceBaseInfo\.luac' /home/opc/lastwar-lua/matches.tsv | cut -f1 | head -4); do
  p=$(find /home/opc/lastwar-lua/fixed -type f -name "$f" | head -1)
  test -n "$p"
  echo "FILE:$f"
  java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$p" | head -620
done
'@
    client_overlay_checks = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/client-overlay.XXXXXX)
trap 'rm -rf "$d"' EXIT
git clone --quiet --depth 1 --branch feature/interactive-continuous-reader https://github.com/mysteryhawk/lastwar-client.git "$d/client"
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/overlay"
cp -R "$d/overlay/." "$d/client/"
cd "$d/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/types.go internal/analytics/config.go internal/analytics/alliance.go internal/analytics/analytics_test.go internal/app/analytics.go
bad=$(/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/analytics/types.go internal/analytics/config.go internal/analytics/alliance.go internal/analytics/analytics_test.go internal/app/analytics.go)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
'@
    client_overlay_gofmt_diff = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/client-gofmt.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/overlay"
cd "$d/overlay"
/home/opc/.local/lib/go1.27.2/bin/gofmt -d internal/analytics/types.go internal/analytics/config.go internal/analytics/alliance.go internal/analytics/analytics_test.go internal/app/analytics.go
'@
    historical_vs_prepare = @'
set -eu
test -s /home/opc/doac-private/doac-history.sqlite3
test -s /home/opc/doac-private/alliance-id
d=$(mktemp -d /tmp/doac-dev/historical-prepare.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$d/overlay"
out=/tmp/doac-dev/historical-vs
mkdir -p "$out"
chmod 700 "$out"
python3 "$d/overlay/.codex-transfer/artifacts/prepare_s3_vs.py" \
  --source "$d/overlay/.codex-transfer/artifacts/doac-s3-vs-source.md" \
  --db /home/opc/doac-private/doac-history.sqlite3 \
  --capture-root /home/opc/doac-private \
  --alliance-id-file /home/opc/doac-private/alliance-id \
  --evidence "$out/evidence.json" --mapping "$out/mapping.json" \
  --unmatched "$out/unmatched.json"
test "$(stat -c %a "$out/evidence.json")" = 600
test "$(stat -c %a "$out/mapping.json")" = 600
'@
    alpha_deploy_diagnose = @'
set -eu
printf 'backup='; test -d /home/opc/doac-private/backups/alpha-candidate-cbf303c-6c060f9 && echo yes || echo no
printf 'dashboard='; systemctl --user is-active doac-dashboard.service || true
for unit in doac-thp-mvp.timer doac-vs-mvp.timer doac-base-level.timer doac-thp-benchmark.timer doac-events.timer; do
  printf '%s=' "$unit"; systemctl --user is-active "$unit" 2>/dev/null || true
done
for bundle in /tmp/doac-dev/incoming/lastwar-client-bundle.zip /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip; do
  printf '%s=' "$(basename "$bundle")"; test -s "$bundle" && echo present || echo missing
  unzip -l "$bundle" 2>/dev/null | grep -E 'client-cbf303c.zip|dashboard-6c060f9.zip' || true
done
python3 - <<'PY'
import sqlite3
db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True)
tables={r[0] for r in db.execute("select name from sqlite_master where type='table'")}
print('historical_tables='+str({'historical_vs_periods','historical_vs_records'} <= tables).lower())
if 'historical_vs_periods' in tables:
 print('historical_periods='+str(db.execute('select count(*) from historical_vs_periods').fetchone()[0]))
print('alliance_detail_snapshots='+str(db.execute("select count(*) from alliance_ranking_snapshots where command='get.al.info'").fetchone()[0]))
PY
'@
    alpha_archive_hashes = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/alpha-hashes.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/client"
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$d/dashboard"
sha256sum "$d/client/.codex-transfer/artifacts/client-cbf303c.zip"
sha256sum "$d/dashboard/.codex-transfer/artifacts/dashboard-6c060f9.zip"
'@
    alpha_stage_checks = @'
set -u
d=$(mktemp -d /tmp/doac-dev/alpha-stage-check.XXXXXX)
trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/cp"
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$d/dp"
unzip -oq "$d/cp/.codex-transfer/artifacts/client-cbf303c.zip" -d "$d/client"
unzip -oq "$d/dp/.codex-transfer/artifacts/dashboard-6c060f9.zip" -d "$d/dashboard"
cd "$d/client"
echo GOFMT
find internal/analytics -type f -name '*.go' -print0 | xargs -0 /home/opc/.local/lib/go1.27.2/bin/gofmt -l
/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/app/analytics.go
echo GOTEST
/home/opc/.local/bin/go test -count=1 ./internal/analytics ./internal/app ./internal/session ./internal/sfs
echo GOVET
/home/opc/.local/bin/go vet ./internal/analytics ./internal/app ./internal/session ./internal/sfs
echo GOBUILD
/home/opc/.local/bin/go build -o "$d/lastwar-client" ./cmd/lastwar-client
cd "$d/dashboard"
echo PYTEST1
/home/opc/doac-dashboard/.venv/bin/python -m unittest tools.test_import_historical_vs dashboard.test_historical_vs dashboard.test_dashboard
cd "$d/dashboard/tools"
echo PYTEST2
/home/opc/doac-dashboard/.venv/bin/python -m unittest test_ingest_analytics test_account_safety test_run_opponent_alliance_details
'@
    alliance_detail_failure_diagnostic = @'
set -eu
python3 - <<'PY'
import glob,json,os
paths=sorted(glob.glob('/home/opc/doac-private/captures/opponent-alliance-details-1724-*.jsonl'))
if not paths: raise SystemExit('no alliance-detail capture')
path=paths[-1]
lines=[line for line in open(path,encoding='utf-8') if line.strip()]
print({'file':os.path.basename(path),'snapshots':len(lines)})
for line in lines:
 doc=json.loads(line)
 print({'status':doc.get('status'),'response_fields':doc.get('response_fields',[]),
        'response_other_fields':doc.get('response_other_fields',0),
        'rejection_reasons':doc.get('rejection_reasons',{})})
PY
'@
    historical_vs_unmatched_candidates = @'
set -eu
python3 - <<'PY'
import difflib,json,sqlite3,unicodedata
from pathlib import Path

def normalise(value):
    value=unicodedata.normalize('NFKD',value.casefold())
    return ''.join(ch for ch in value if ch.isalnum() and not unicodedata.combining(ch))

unmatched=json.loads(Path('/tmp/doac-dev/historical-vs/unmatched.json').read_text(encoding='utf-8'))
db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True)
row=db.execute("""SELECT id FROM analytics_snapshots
 WHERE target_server=1723 AND metric='THP' AND command='dragon.assign.player.info'
 AND status='ok' AND validation IN ('manual_100_name_set_verified','uid_continuity_verified')
 ORDER BY collected_at DESC,id DESC LIMIT 1""").fetchone()
if not row: raise SystemExit('no validated current THP snapshot')
names=sorted({r[0] for r in db.execute('SELECT name FROM analytics_records WHERE snapshot_id=?',(row[0],)) if r[0]})
for old in unmatched.get('unmatched',[]):
    key=normalise(old)
    ranked=sorted(names,key=lambda name:(-difflib.SequenceMatcher(None,key,normalise(name)).ratio(),name.casefold()))[:4]
    print(json.dumps({'historical_name':old,'possible_current_names':ranked},ensure_ascii=False))
print(json.dumps({'unmatched_count':len(unmatched.get('unmatched',[])),'ambiguous_count':len(unmatched.get('ambiguous',[]))}))
PY
'@
    lua_create_server_trace = @'
set -eu
grep -RInE 'createServer|ownerServerId' /home/opc/lastwar-lua/decompiled 2>/dev/null | head -320 || true
for f in $(find /home/opc/lastwar-lua/fixed -type f -name '*.luac'); do
  if strings "$f" | grep -q '^createServer$'; then
    b=$(basename "$f")
    echo "FILE:$b"
    grep -F "$b" /home/opc/lastwar-lua/matches.tsv || true
    java -jar /home/opc/lastwar-lua/unluac/bin/unluac.jar "$f" 2>/dev/null | grep -C 8 -E 'createServer|ownerServerId' | head -160 || true
  fi
done
'@
    alliance_detail_fix_state = @'
set -eu
printf 'bundle='; test -s /tmp/doac-dev/incoming/lastwar-client-bundle.zip && echo present || echo missing
unzip -l /tmp/doac-dev/incoming/lastwar-client-bundle.zip 2>/dev/null | grep -E 'client-15fe875.zip' || true
d=$(mktemp -d /tmp/doac-dev/detail-fix-inspect.XXXXXX); trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d"
sha256sum "$d/.codex-transfer/artifacts/client-15fe875.zip"
printf 'marker='; test -e /home/opc/doac-private/captures/alliance-detail-fix-15fe875.attempted && echo present || echo absent
printf 'backup='; test -e /home/opc/doac-private/backups/alliance-detail-fix-15fe875-predeploy && echo present || echo absent
free -m | awk 'NR==2 {print "available_mb=" $7}'
df -Pk /home/opc | awk 'NR==2 {print "available_kb=" $4}'
for timer in doac-thp-mvp.timer doac-vs-mvp.timer doac-base-level.timer doac-thp-benchmark.timer doac-events.timer; do printf '%s=' "$timer"; systemctl --user is-active "$timer" 2>/dev/null || true; done
for service in doac-thp-mvp.service doac-vs-mvp.service doac-base-level.service doac-thp-benchmark.service doac-events.service; do printf '%s=' "$service"; systemctl --user is-active "$service" 2>/dev/null || true; done
'@
    alliance_detail_fix_gofmt = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/detail-fix-gofmt.XXXXXX); trap 'rm -rf "$d"' EXIT
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$d/packages"
unzip -oq "$d/packages/.codex-transfer/artifacts/client-15fe875.zip" -d "$d/client"
cd "$d/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -d internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/types.go
'@
    dashboard_contract_overlay_checks = @'
set -eu
d=$(mktemp -d /tmp/doac-dev/dashboard-contract.XXXXXX); trap 'rm -rf "$d"' EXIT
cp -a /home/opc/doac-dashboard "$d/dashboard"
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$d/overlay"
for path in tools/ingest_analytics.py tools/test_ingest_analytics.py tools/test_import_historical_vs.py tools/test_account_safety.py tools/test_run_opponent_alliance_details.py dashboard/test_historical_vs.py docs/SNAPSHOT-CONTRACT.md; do
  install -m 0644 "$d/overlay/$path" "$d/dashboard/$path"
done
cd "$d/dashboard"
/home/opc/doac-dashboard/.venv/bin/python -m unittest tools.test_import_historical_vs dashboard.test_historical_vs dashboard.test_dashboard
cd "$d/dashboard/tools"
/home/opc/doac-dashboard/.venv/bin/python -m unittest test_ingest_analytics test_account_safety test_run_opponent_alliance_details
/home/opc/doac-dashboard/.venv/bin/python -m py_compile ingest_analytics.py test_ingest_analytics.py
'@
    opponent_1724_read_model = @'
set -eu
cd /home/opc/doac-dashboard
/home/opc/doac-dashboard/.venv/bin/python - <<'PY'
from dashboard import history
with history.connect('/home/opc/doac-private/doac-history.sqlite3') as db:
    data=history.opponent_scout(db,1724,'thp')
rows=[]
for row in data['alliances']:
    if row.get('alliance_name'):
        rows.append({'name':row['alliance_name'],'abbr':row.get('alliance_abbr'),
                     'total_power':row.get('total_power'),'member_count':row.get('member_count'),
                     'average_power':row.get('average_power'),
                     'top200_thp_count':row.get('top200_thp_count'),
                     'top_five_count':len(row.get('top_five_thp') or [])})
print({'cards':len(data['alliances']),'named_cards':len(rows),'alliances':rows})
PY
curl -sS -o /dev/null -w 'opponents_http=%{http_code}\n' 'http://127.0.0.1:8080/opponents?server=1724'
'@
}
$operation = [string]$manifest.operation
if (!$commands.ContainsKey($operation)) { throw 'unsupported read-only inspection operation' }

foreach ($required in @($keyPath, $knownHostsPath, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

& $ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHostsPath" -- 'opc@140.238.71.236' $commands[$operation]
if ($LASTEXITCODE -ne 0) { throw "read-only VM inspection failed with exit code $LASTEXITCODE" }
