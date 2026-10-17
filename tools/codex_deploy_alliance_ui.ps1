$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed recovery for the existing Cloudflare quick-tunnel service. It accepts no
# arguments, opens no ports, and verifies a newly issued public login endpoint.
if ($args.Count -ne 0) { throw 'arguments are not permitted' }
$key = 'C:\Users\edwar\Downloads\ssh-key-2026-10-10.key.txt'
$knownHosts = 'C:\Users\edwar\.ssh\known_hosts'
$ssh = 'C:\Windows\System32\OpenSSH\ssh.exe'
foreach ($required in @($key, $knownHosts, $ssh)) {
    if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "required file is unavailable: $required" }
}

$remote = @'
set -eu
log=/home/opc/doac-private/dashboard-tunnel.log
old=$(grep -Eo 'https://[A-Za-z0-9.-]+\.trycloudflare\.com' "$log" 2>/dev/null | tail -1 || true)
systemctl --user restart doac-dashboard-tunnel.service
new=''
for n in $(seq 1 30); do
  new=$(grep -Eo 'https://[A-Za-z0-9.-]+\.trycloudflare\.com' "$log" 2>/dev/null | tail -1 || true)
  if test -n "$new" && test "$new" != "$old"; then break; fi
  sleep 2
done
test -n "$new"
test "$new" != "$old"
code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "$new/login")
printf '%s\n%s\n' "$new" "$code"
test "$code" = 200
systemctl --user is-active doac-dashboard-tunnel.service
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "dashboard tunnel recovery failed with code $LASTEXITCODE" }
