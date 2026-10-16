$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# One reviewed deployment for dashboard commit 3bb6f7e. No caller arguments,
# game access, database writes, firewall changes or arbitrary remote command input.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$key = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHosts = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
foreach ($required in @($key, $knownHosts, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

$remote = @'
set -eu
test -s /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'
stage=$(mktemp -d /tmp/doac-dev/alliance-ui.XXXXXX)
trap 'rm -rf "$stage"' EXIT
cp -a /home/opc/doac-dashboard/. "$stage"/
unzip -oq /tmp/doac-dev/incoming/lastwar-dashboard-bundle.zip -d "$stage"
cd "$stage"
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s tools -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s dashboard -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m py_compile dashboard/app.py dashboard/history.py tools/ingest_analytics.py tools/run_thp_benchmark.py
backup=/home/opc/doac-private/backups/alliance-ui-3bb6f7e-predeploy
test ! -e "$backup"
umask 077
mkdir -p "$backup"
tar -C /home/opc/doac-dashboard -czf "$backup/dashboard-code.tar.gz" dashboard tools docs 2>/dev/null
for path in \
 dashboard/README.md dashboard/app.py dashboard/history.py dashboard/static/dashboard.css \
 dashboard/templates/base.html dashboard/templates/compare.html \
 dashboard/templates/opponent_alliance.html dashboard/templates/opponents.html \
 dashboard/test_dashboard.py docs/DOAC-AGENT-STATE.md docs/SNAPSHOT-CONTRACT.md \
 tools/ingest_analytics.py tools/run_thp_benchmark.py tools/test_ingest_analytics.py \
 tools/test_run_thp_benchmark.py; do
  install -m 0644 "$stage/$path" "/home/opc/doac-dashboard/$path"
done
systemctl --user restart doac-dashboard.service
for n in 1 2 3 4 5 6 7 8; do
  if curl -fsS http://127.0.0.1:8080/health >/dev/null; then
    systemctl --user is-active doac-dashboard.service
    exit 0
  fi
  sleep 2
done
systemctl --user --no-pager --full status doac-dashboard.service | tail -80
exit 1
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "bounded dashboard deployment failed with exit code $LASTEXITCODE" }
