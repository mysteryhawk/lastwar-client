$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed one-use deployment for the reviewed createServer fallback at client commit
# 15fe875. No caller arguments, credential output, authentication retries or gameplay
# commands. It builds in scratch, preserves the previous binary, and makes one guarded
# read-only foreign-alliance detail request through the shared account-safety runner.
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
archive_name=client-15fe875.zip
archive_sha=ee9a23deaa8a1bfb55b8b717587f92d8a707094c7b47a098ace01ef3f9a6f603
marker=/home/opc/doac-private/captures/alliance-detail-fix-15fe875.attempted
backup=/home/opc/doac-private/backups/alliance-detail-fix-15fe875-predeploy
test -s "$bundle"
test ! -e "$marker"
test ! -e "$backup"
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'

timers='doac-thp-mvp.timer doac-vs-mvp.timer doac-base-level.timer doac-thp-benchmark.timer doac-events.timer'
active_timers=''
root=$(mktemp -d /tmp/doac-dev/alliance-detail-fix.XXXXXX)
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

mkdir -p "$root/packages" "$root/client"
unzip -oq "$bundle" -d "$root/packages"
archive="$root/packages/.codex-transfer/artifacts/$archive_name"
test -s "$archive"
test "$(sha256sum "$archive" | cut -d' ' -f1)" = "$archive_sha"
unzip -oq "$archive" -d "$root/client"
cd "$root/client"
find internal -type f -name '*.go' -print0 | xargs -0 /home/opc/.local/lib/go1.27.2/bin/gofmt -w
bad=$(find internal -type f -name '*.go' -print0 | xargs -0 /home/opc/.local/lib/go1.27.2/bin/gofmt -l)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
/home/opc/.local/bin/go build -o "$root/lastwar-client" ./cmd/lastwar-client
test -x "$root/lastwar-client"

umask 077
mkdir -p "$backup"
cp -p /home/opc/doac-private/bin/lastwar-client "$backup/active-lastwar-client"
install -m 0700 "$root/lastwar-client" /home/opc/doac-private/bin/lastwar-client
touch "$marker"

set +e
DOAC_ACCOUNT_SAFETY_DIR=/home/opc/doac-private/account-safety \
  /home/opc/doac-dashboard/.venv/bin/python /home/opc/doac-dashboard/tools/run_opponent_alliance_details.py \
  --client /home/opc/doac-private/bin/lastwar-client \
  --session-config /home/opc/doac-private/session.json \
  --alliance-id-file /home/opc/doac-private/alliance-id \
  --private-dir /home/opc/doac-private/captures \
  --db /home/opc/doac-private/doac-history.sqlite3 \
  --target-server 1724 --limit 1 --timeout 300
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
if ($LASTEXITCODE -ne 0) { throw "bounded alliance-detail deployment exited with code $LASTEXITCODE" }
