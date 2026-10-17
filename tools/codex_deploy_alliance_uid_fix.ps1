$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed one-use deployment for dashboard commit 52f2316. It installs only the
# reviewed importer contract change and makes one guarded, read-only 1724 detail
# collection. It accepts no arguments and performs no authentication retries.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$key = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHosts = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
foreach ($required in @($key, $knownHosts, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

$remote = @'
set -eu
bundle=/tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip
archive_name=dashboard-52f2316.zip
archive_sha=9d1abee18f9e97f6a4f5706dc5cec9797de5ec0548659972057067764f9711ce
marker=/home/opc/doac-private/captures/alliance-detail-ingest-52f2316.attempted
backup=/home/opc/doac-private/backups/alliance-detail-ingest-52f2316-predeploy
live=/home/opc/doac-dashboard
db=/home/opc/doac-private/doac-history.sqlite3
test -s "$bundle"
test ! -e "$marker"
test ! -e "$backup"
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'

timers='doac-thp-mvp.timer doac-vs-mvp.timer doac-base-level.timer doac-thp-benchmark.timer doac-events.timer'
active_timers=''
root=$(mktemp -d /tmp/doac-dev/alliance-detail-ingest.XXXXXX)
cleanup() {
  code=$?
  trap - EXIT
  for timer in $active_timers; do systemctl --user start "$timer" || true; done
  rm -rf "$root"
  exit "$code"
}
trap cleanup EXIT
for timer in $timers; do
  if systemctl --user is-active --quiet "$timer"; then
    active_timers="$active_timers $timer"
    systemctl --user stop "$timer"
  fi
done
for service in doac-thp-mvp.service doac-vs-mvp.service doac-base-level.service doac-thp-benchmark.service doac-events.service; do
  if systemctl --user is-active --quiet "$service"; then
    echo "collector is active: $service"
    exit 5
  fi
done

mkdir -p "$root/packages" "$root/dashboard"
unzip -oq "$bundle" -d "$root/packages"
archive="$root/packages/.codex-transfer/artifacts/$archive_name"
test -s "$archive"
test "$(sha256sum "$archive" | cut -d' ' -f1)" = "$archive_sha"
unzip -oq "$archive" -d "$root/dashboard"
cd "$root/dashboard"
/home/opc/doac-dashboard/.venv/bin/python -m unittest tools.test_import_historical_vs dashboard.test_historical_vs dashboard.test_dashboard
cd "$root/dashboard/tools"
/home/opc/doac-dashboard/.venv/bin/python -m unittest test_ingest_analytics test_account_safety test_run_opponent_alliance_details
/home/opc/doac-dashboard/.venv/bin/python -m py_compile ingest_analytics.py test_ingest_analytics.py

umask 077
mkdir -p "$backup"
cp -p "$live/tools/ingest_analytics.py" "$backup/ingest_analytics.py"
python3 - <<'PY'
import os,sqlite3
source=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True)
target=sqlite3.connect('/home/opc/doac-private/backups/alliance-detail-ingest-52f2316-predeploy/doac-history.sqlite3')
source.backup(target);target.close();source.close()
os.chmod('/home/opc/doac-private/backups/alliance-detail-ingest-52f2316-predeploy/doac-history.sqlite3',0o600)
PY
install -m 0644 "$root/dashboard/tools/ingest_analytics.py" "$live/tools/ingest_analytics.py"
touch "$marker"

set +e
DOAC_ACCOUNT_SAFETY_DIR=/home/opc/doac-private/account-safety \
  /home/opc/doac-dashboard/.venv/bin/python "$live/tools/run_opponent_alliance_details.py" \
  --client /home/opc/doac-private/bin/lastwar-client \
  --session-config /home/opc/doac-private/session.json \
  --alliance-id-file /home/opc/doac-private/alliance-id \
  --private-dir /home/opc/doac-private/captures \
  --db "$db" --target-server 1724 --limit 1 --timeout 300
detail_exit=$?
set -e

python3 - <<'PY'
import json,sqlite3
db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True)
row=db.execute("""select count(*),count(distinct r.alliance_id),
 sum(case when r.name<>'' then 1 else 0 end),
 sum(case when r.member_count is not null then 1 else 0 end),
 sum(case when r.total_power>0 then 1 else 0 end)
 from alliance_ranking_records r join alliance_ranking_snapshots s on s.id=r.snapshot_id
 where s.target_server=1724 and s.command='get.al.info'""").fetchone()
print(json.dumps({'foreign_alliance_details':row}))
PY
printf 'opponent_detail_exit=%s\n' "$detail_exit"
systemctl --user is-active doac-dashboard.service
exit "$detail_exit"
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "bounded alliance-detail importer deployment exited with code $LASTEXITCODE" }
