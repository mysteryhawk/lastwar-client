$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# One reviewed deployment for client a1522ef and dashboard 9e67051. No caller
# arguments, game access, credential output, firewall changes or arbitrary
# remote command input. The SQLite change is additive schema creation only.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$key = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHosts = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
foreach ($required in @($key, $knownHosts, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

$remote = @'
set -eu
test -s /tmp/doac-dev/incoming/lastwar-client-bundle.zip
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'
root=$(mktemp -d /tmp/doac-dev/alliance-power.XXXXXX)
trap 'rm -rf "$root"' EXIT
client="$root/client"
dashboard="$root/dashboard"
packages="$root/packages"
mkdir -p "$client" "$dashboard" "$packages"
unzip -oq /tmp/doac-dev/incoming/lastwar-client-bundle.zip -d "$packages"
test -s "$packages/.codex-transfer/artifacts/client-a1522ef.zip"
test -s "$packages/.codex-transfer/artifacts/dashboard-9e67051.zip"
unzip -oq "$packages/.codex-transfer/artifacts/client-a1522ef.zip" -d "$client"
unzip -oq "$packages/.codex-transfer/artifacts/dashboard-9e67051.zip" -d "$dashboard"
install -m 0644 "$packages/internal/analytics/alliance.go" "$client/internal/analytics/alliance.go"
install -m 0644 "$packages/internal/analytics/analytics_test.go" "$client/internal/analytics/analytics_test.go"
install -m 0644 "$packages/internal/analytics/types.go" "$client/internal/analytics/types.go"

cd "$client"
/home/opc/.local/lib/go1.27.2/bin/gofmt -w internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/config.go internal/analytics/types.go internal/app/analytics.go internal/sfs/sfsobject_sensitive_keys_sync_test.go
bad=$(/home/opc/.local/lib/go1.27.2/bin/gofmt -l internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/config.go internal/analytics/types.go internal/app/analytics.go internal/sfs/sfsobject_sensitive_keys_sync_test.go)
test -z "$bad"
/home/opc/.local/bin/go test -count=1 ./...
/home/opc/.local/bin/go vet ./...
/home/opc/.local/bin/go build -o "$root/lastwar-client" ./cmd/lastwar-client
test -x "$root/lastwar-client"

cd "$dashboard"
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s tools -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s dashboard -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m py_compile dashboard/app.py dashboard/history.py tools/ingest_analytics.py tools/run_alliance_power.py

backup=/home/opc/doac-private/backups/alliance-power-9e67051-predeploy-v6
test ! -e "$backup"
umask 077
mkdir -p "$backup/client" "$backup/dashboard"
cp -p /home/opc/lastwar-client/lastwar-client "$backup/client/lastwar-client"
cp -p /home/opc/doac-private/bin/lastwar-client "$backup/client/active-lastwar-client"
for path in internal/analytics/alliance.go internal/analytics/analytics_test.go internal/analytics/config.go internal/analytics/types.go internal/app/analytics.go internal/sfs/sfsobject_sensitive_keys_sync_test.go; do
  if test -f "/home/opc/lastwar-client/$path"; then
    mkdir -p "$backup/client/$(dirname "$path")"
    cp -p "/home/opc/lastwar-client/$path" "$backup/client/$path"
  fi
done
for path in dashboard/README.md dashboard/history.py dashboard/templates/compare.html dashboard/templates/opponent_alliance.html dashboard/test_dashboard.py docs/DOAC-AGENT-STATE.md docs/SNAPSHOT-CONTRACT.md tools/ingest_analytics.py tools/run_alliance_power.py tools/test_ingest_analytics.py tools/test_run_alliance_power.py; do
  if test -f "/home/opc/doac-dashboard/$path"; then
    mkdir -p "$backup/dashboard/$(dirname "$path")"
    cp -p "/home/opc/doac-dashboard/$path" "$backup/dashboard/$path"
  fi
done

install -m 0755 "$root/lastwar-client" /home/opc/lastwar-client/lastwar-client
install -m 0700 "$root/lastwar-client" /home/opc/doac-private/bin/lastwar-client
for path in dashboard/README.md dashboard/history.py dashboard/templates/compare.html dashboard/templates/opponent_alliance.html dashboard/test_dashboard.py docs/DOAC-AGENT-STATE.md docs/SNAPSHOT-CONTRACT.md tools/ingest_analytics.py tools/run_alliance_power.py tools/test_ingest_analytics.py tools/test_run_alliance_power.py; do
  install -m 0644 "$dashboard/$path" "/home/opc/doac-dashboard/$path"
done

cd /home/opc/doac-dashboard
/home/opc/doac-dashboard/.venv/bin/python -c "import sqlite3; from tools.ingest_analytics import SCHEMA; db=sqlite3.connect('/home/opc/doac-private/doac-history.sqlite3'); db.executescript(SCHEMA); db.close()"
systemctl --user restart doac-dashboard.service
for n in 1 2 3 4 5 6 7 8; do
  if curl -fsS http://127.0.0.1:8080/health >/dev/null; then
    systemctl --user is-active doac-dashboard.service
    python3 -c "import sqlite3; db=sqlite3.connect('file:/home/opc/doac-private/doac-history.sqlite3?mode=ro',uri=True); print([r[0] for r in db.execute(\"select name from sqlite_master where type='table' and name like 'alliance_ranking_%' order by name\")])"
    break
  fi
  sleep 2
done
curl -fsS http://127.0.0.1:8080/health >/dev/null || {
  systemctl --user --no-pager --full status doac-dashboard.service | tail -80
  exit 1
}
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "bounded alliance-power deployment failed with exit code $LASTEXITCODE" }
