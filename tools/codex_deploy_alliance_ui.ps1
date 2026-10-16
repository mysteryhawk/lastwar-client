$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed no-argument deployment of the reviewed alliance-rank UI overlay. It
# neither contacts the game nor changes the database, timers, tunnel or ports.
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
backup=/home/opc/doac-private/backups/alliance-rank-ui-predeploy
test -s "$bundle"
test ! -e "$backup"
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'
root=$(mktemp -d /tmp/doac-dev/alliance-rank-ui.XXXXXX)
trap 'rm -rf "$root"' EXIT
cp -a /home/opc/doac-dashboard/. "$root/dashboard"
unzip -oq "$bundle" -d "$root/overlay"
paths='dashboard/history.py dashboard/test_dashboard.py dashboard/templates/compare.html dashboard/templates/opponents.html'
for path in $paths; do
  test -s "$root/overlay/$path"
  install -m 0644 "$root/overlay/$path" "$root/dashboard/$path"
done
cd "$root/dashboard"
/home/opc/doac-dashboard/.venv/bin/python -m unittest discover -s tools -p 'test_*.py'
/home/opc/doac-dashboard/.venv/bin/python -m unittest dashboard.test_dashboard
/home/opc/doac-dashboard/.venv/bin/python -m py_compile dashboard/history.py dashboard/test_dashboard.py
umask 077
mkdir -p "$backup"
for path in $paths; do
  mkdir -p "$backup/$(dirname "$path")"
  cp -p "/home/opc/doac-dashboard/$path" "$backup/$path"
  install -m 0644 "$root/overlay/$path" "/home/opc/doac-dashboard/$path"
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
if ($LASTEXITCODE -ne 0) { throw "bounded alliance-rank UI deployment failed with exit code $LASTEXITCODE" }
