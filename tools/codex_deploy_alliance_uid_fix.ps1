$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed one-use deployment and import for live-verified alliance UID commit
# cfdded6. No caller arguments, gameplay commands or authentication retries.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$key = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHosts = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
foreach ($required in @($key, $knownHosts, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

$remote = @'
set -eu
bundle=/tmp/doac-dev/incoming/lastwar-client-bundle.zip
marker=/home/opc/doac-private/captures/alliance-uid-fix-cfdded6.attempted
backup=/home/opc/doac-private/backups/alliance-uid-fix-cfdded6-predeploy
recovery=/home/opc/doac-private/captures/alliance-uid-fix-cfdded6.recovered
installed=/home/opc/doac-private/captures/alliance-clean-export.installed
rank_installed=/home/opc/doac-private/captures/alliance-array-rank.installed
capture=/home/opc/doac-private/captures/alliance-power-20261016T085756653799Z.jsonl
db=/home/opc/doac-private/doac-history.sqlite3
if test -e "$marker" && test ! -e "$recovery"; then
  test -d "$backup"
  test ! -e "$recovery"
  test -s "$capture"
  cd /home/opc/doac-dashboard
  /home/opc/doac-dashboard/.venv/bin/python - <<'PY'
import json, os, sqlite3
from pathlib import Path
from tools.ingest_analytics import SCHEMA, ingest_alliance_rankings, validate_alliance

capture=Path('/home/opc/doac-private/captures/alliance-power-20261016T085756653799Z.jsonl')
db_path=Path('/home/opc/doac-private/doac-history.sqlite3')
backup=Path('/home/opc/doac-private/backups/alliance-uid-fix-cfdded6-predeploy/doac-history-pre-alliance-import.sqlite3')
lines=capture.read_text(encoding='utf-8').splitlines()
if len(lines)!=1:
    raise SystemExit('retained capture must contain exactly one snapshot')
doc=json.loads(lines[0])
for key in ('response_fields','response_other_fields','self_ranking_type',
            'rejection_reasons','row_shape'):
    doc.pop(key,None)
alliance_id=Path('/home/opc/doac-private/alliance-id').read_text(encoding='utf-8').strip()
snapshot=validate_alliance(doc)
if (snapshot['source_server']!=1723 or snapshot['target_server']!=1723 or
        snapshot['tracked_alliance_id']!=alliance_id or snapshot['status']!='ok' or
        snapshot['rejected_rows']!=0 or len(snapshot['alliances'])!=50 or
        sum(r['alliance_id']==alliance_id for r in snapshot['alliances'])!=1):
    raise SystemExit('retained alliance snapshot failed guarded recovery checks')
if backup.exists():
    raise SystemExit('database recovery backup already exists')
source=sqlite3.connect(f'file:{db_path}?mode=ro',uri=True)
target=sqlite3.connect(backup)
source.backup(target)
target.close(); source.close()
os.chmod(backup,0o600)
with sqlite3.connect(db_path) as db:
    db.executescript(SCHEMA)
    added=ingest_alliance_rankings(db,[json.dumps(snapshot,separators=(',',':'))])
    if added!=1:
        raise SystemExit('retained alliance snapshot was not inserted')
latest=capture.parent/'alliance-power-latest.jsonl'
latest.write_text(json.dumps(snapshot,separators=(',',':'))+'\n',encoding='utf-8')
os.chmod(latest,0o600)
doac=[r for r in snapshot['alliances'] if r['alliance_id']==alliance_id][0]
print({'status':'ok','alliance_count':len(snapshot['alliances']),
       'tracked':{'name':doac['name'],'abbr':doac['abbr'],'rank':doac.get('rank'),
                  'total_power':doac['total_power']}})
PY
  touch "$recovery"
  exit 0
fi
if test -e "$marker" && test -e "$recovery"; then
  if test -e "$installed"; then
    test ! -e "$rank_installed"
    test -s "$bundle"
    rank_backup=/home/opc/doac-private/backups/alliance-array-rank-predeploy
    test ! -e "$rank_backup"
    root=$(mktemp -d /tmp/doac-dev/alliance-rank-install.XXXXXX)
    trap 'rm -rf "$root"' EXIT
    git clone --quiet --depth 1 --branch feature/interactive-continuous-reader https://github.com/mysteryhawk/lastwar-client.git "$root/client"
    unzip -oq "$bundle" -d "$root/overlay"
    install -m 0644 "$root/overlay/internal/analytics/alliance.go" "$root/client/internal/analytics/alliance.go"
    install -m 0644 "$root/overlay/internal/analytics/analytics_test.go" "$root/client/internal/analytics/analytics_test.go"
    cd "$root/client"
    /home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go
    /home/opc/.local/bin/go test -count=1 ./internal/analytics ./internal/app ./internal/sfs
    /home/opc/.local/bin/go vet ./internal/analytics ./internal/app ./internal/sfs
    /home/opc/.local/bin/go build -o "$root/lastwar-client" ./cmd/lastwar-client
    mkdir -p "$rank_backup"
    cp -p /home/opc/doac-private/bin/lastwar-client "$rank_backup/active-lastwar-client"
    cd /home/opc/doac-dashboard
    /home/opc/doac-dashboard/.venv/bin/python - <<'PY'
import json, os, sqlite3
from pathlib import Path
from tools.ingest_analytics import validate_alliance

latest=Path('/home/opc/doac-private/captures/alliance-power-latest.jsonl')
db_path=Path('/home/opc/doac-private/doac-history.sqlite3')
backup=Path('/home/opc/doac-private/backups/alliance-array-rank-predeploy/doac-history-pre-rank.sqlite3')
lines=latest.read_text(encoding='utf-8').splitlines()
if len(lines)!=1:
    raise SystemExit('latest alliance snapshot must contain exactly one line')
doc=validate_alliance(json.loads(lines[0]))
if len(doc['alliances'])!=50 or any(r.get('rank') is not None for r in doc['alliances']):
    raise SystemExit('alliance snapshot is not the expected unranked 50-row cohort')
for rank,row in enumerate(doc['alliances'],1):
    row['rank']=rank
doc=validate_alliance(doc)
aid=Path('/home/opc/doac-private/alliance-id').read_text(encoding='utf-8').strip()
source=sqlite3.connect(f'file:{db_path}?mode=ro',uri=True)
target=sqlite3.connect(backup); source.backup(target); target.close(); source.close()
os.chmod(backup,0o600)
with sqlite3.connect(db_path) as db:
    db.row_factory=sqlite3.Row
    snap=db.execute("select id from alliance_ranking_snapshots where target_server=1723 order by collected_at desc,id desc limit 1").fetchone()
    rows=db.execute('select alliance_id,rank from alliance_ranking_records where snapshot_id=?',(snap['id'],)).fetchall()
    if len(rows)!=50 or {r['alliance_id'] for r in rows}!={r['alliance_id'] for r in doc['alliances']} or any(r['rank'] is not None for r in rows):
        raise SystemExit('database alliance cohort is not the expected unranked snapshot')
    with db:
        for row in doc['alliances']:
            db.execute('update alliance_ranking_records set rank=? where snapshot_id=? and alliance_id=?',
                       (row['rank'],snap['id'],row['alliance_id']))
latest.write_text(json.dumps(doc,separators=(',',':'))+'\n',encoding='utf-8')
os.chmod(latest,0o600)
tracked=next(r for r in doc['alliances'] if r['alliance_id']==aid)
print({'status':'ok','alliance_count':len(doc['alliances']),
       'tracked':{'name':tracked['name'],'abbr':tracked['abbr'],'rank':tracked['rank'],
                  'total_power':tracked['total_power']}})
PY
    install -m 0700 "$root/lastwar-client" /home/opc/doac-private/bin/lastwar-client
    touch "$rank_installed"
    echo 'alliance array-rank collector installed; retained snapshot backfilled; no game request sent'
    exit 0
  fi
  test -s "$bundle"
  clean_backup=/home/opc/doac-private/backups/alliance-clean-export-predeploy
  test ! -e "$clean_backup"
  root=$(mktemp -d /tmp/doac-dev/alliance-clean-install.XXXXXX)
  trap 'rm -rf "$root"' EXIT
  git clone --quiet --depth 1 --branch feature/interactive-continuous-reader https://github.com/mysteryhawk/lastwar-client.git "$root/client"
  unzip -oq "$bundle" -d "$root/overlay"
  install -m 0644 "$root/overlay/internal/analytics/alliance.go" "$root/client/internal/analytics/alliance.go"
  install -m 0644 "$root/overlay/internal/analytics/analytics_test.go" "$root/client/internal/analytics/analytics_test.go"
  cd "$root/client"
  /home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go
  /home/opc/.local/bin/go test -count=1 ./internal/analytics ./internal/app ./internal/sfs
  /home/opc/.local/bin/go vet ./internal/analytics ./internal/app ./internal/sfs
  /home/opc/.local/bin/go build -o "$root/lastwar-client" ./cmd/lastwar-client
  mkdir -p "$clean_backup"
  cp -p /home/opc/doac-private/bin/lastwar-client "$clean_backup/active-lastwar-client"
  install -m 0700 "$root/lastwar-client" /home/opc/doac-private/bin/lastwar-client
  touch "$installed"
  echo 'clean alliance export collector installed; no game request sent'
  exit 0
fi
test -s "$bundle"
test ! -e "$backup"
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'
for unit in doac-thp-mvp.service doac-vs-mvp.service doac-base-level.service doac-thp-benchmark.service; do
  if systemctl --user is-active --quiet "$unit"; then
    echo "collector already active: $unit"
    exit 5
  fi
done

root=$(mktemp -d /tmp/doac-dev/alliance-uid-fix.XXXXXX)
trap 'rm -rf "$root"' EXIT
mkdir -p "$root/packages" "$root/client"
unzip -oq "$bundle" -d "$root/packages"
test -s "$root/packages/.codex-transfer/artifacts/client-cfdded6.zip"
unzip -oq "$root/packages/.codex-transfer/artifacts/client-cfdded6.zip" -d "$root/client"
cd "$root/client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/types.go
bad=$(/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/types.go)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./internal/analytics ./internal/app ./internal/sfs
/home/opc/.local/bin/go vet ./internal/analytics ./internal/app ./internal/sfs
/home/opc/.local/bin/go build -o "$root/lastwar-client" ./cmd/lastwar-client
test -x "$root/lastwar-client"

umask 077
mkdir -p "$backup"
cp -p /home/opc/doac-private/bin/lastwar-client "$backup/active-lastwar-client"
install -m 0700 "$root/lastwar-client" /home/opc/doac-private/bin/lastwar-client
touch "$marker"

cd /home/opc/doac-dashboard
/home/opc/doac-dashboard/.venv/bin/python tools/run_alliance_power.py \
  --client /home/opc/doac-private/bin/lastwar-client \
  --session-config /home/opc/doac-private/session.json \
  --alliance-id-file /home/opc/doac-private/alliance-id \
  --private-dir /home/opc/doac-private/captures \
  --db /home/opc/doac-private/doac-history.sqlite3 \
  --timeout 300
python3 - <<'PY'
import sqlite3
aid=open('/home/opc/doac-private/alliance-id',encoding='utf-8').read().strip()
db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True)
db.row_factory=sqlite3.Row
snap=db.execute('select id,status,collected_at from alliance_ranking_snapshots order by collected_at desc limit 1').fetchone()
row=db.execute('select name,abbr,rank,total_power from alliance_ranking_records where snapshot_id=? and alliance_id=?',(snap['id'],aid)).fetchone()
count=db.execute('select count(*) from alliance_ranking_records where snapshot_id=?',(snap['id'],)).fetchone()[0]
print({'status':snap['status'],'collected_at':snap['collected_at'],'alliance_count':count,
       'tracked':dict(row) if row else None})
PY
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "bounded alliance UID deployment/import exited with code $LASTEXITCODE" }
