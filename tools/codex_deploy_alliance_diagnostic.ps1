$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Fixed one-use deployment and value-free diagnostic for client commit 6d9c809.
# No caller arguments, database writes, gameplay commands or authentication retries.
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
marker=/home/opc/doac-private/captures/alliance-diagnostic-6d9c809.attempted
backup=/home/opc/doac-private/backups/alliance-diagnostic-6d9c809-predeploy
test -s "$bundle"
test ! -e "$marker"
test ! -e "$backup"
free -m | awk 'NR==2 {if ($7 < 300) exit 1}'
df -Pk /home/opc | awk 'NR==2 {if ($4 < 524288) exit 1}'
for unit in doac-thp-mvp.service doac-vs-mvp.service doac-base-level.service doac-thp-benchmark.service; do
  if systemctl --user is-active --quiet "$unit"; then
    echo "collector already active: $unit"
    exit 5
  fi
done

root=$(mktemp -d /tmp/doac-dev/alliance-diagnostic.XXXXXX)
config=''
trap 'test -z "$config" || rm -f "$config"; rm -rf "$root"' EXIT
mkdir -p "$root/packages" "$root/client"
unzip -oq "$bundle" -d "$root/packages"
test -s "$root/packages/.codex-transfer/artifacts/client-6d9c809.zip"
unzip -oq "$root/packages/.codex-transfer/artifacts/client-6d9c809.zip" -d "$root/client"
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

config=$(mktemp /home/opc/doac-private/captures/alliance-diagnostic.XXXXXX.config.json)
output=${config%.config.json}.jsonl
python3 - "$config" "$output" <<'PY'
import json,sys
alliance_id=open('/home/opc/doac-private/alliance-id',encoding='utf-8').read().strip()
json.dump({'target_server':1723,'alliance_id':alliance_id,'output':sys.argv[2],
           'queries':[{'kind':'alliance_power','period':'diagnostic:2026-10-16','ismerge':0}]},
          open(sys.argv[1],'w',encoding='utf-8'),separators=(',',':'))
PY
chmod 0600 "$config"
touch "$marker"
set +e
/home/opc/doac-private/bin/lastwar-client -config /home/opc/doac-private/session.json -analytics "$config"
code=$?
set -e
if test -s "$output"; then
  python3 - "$output" <<'PY'
import json,sys
d=json.loads(open(sys.argv[1],encoding='utf-8').readline())
s=d.get('row_shape') or {}
known_reasons=('wrong_row_type','missing_alliance_id','duplicate_alliance_id',
               'missing_or_wrong_power_type','negative_power','server_mismatch')
out={'status':d.get('status'),'rejected_rows':d.get('rejected_rows'),
     'rejection_reasons':{k:(d.get('rejection_reasons') or {}).get(k,0) for k in known_reasons},
     'self_ranking_type':d.get('self_ranking_type'),
     'row_shape':{'collection_type':s.get('collection_type'),'item_count':s.get('item_count'),
                  'rows_examined':s.get('rows_examined'),'item_types':s.get('item_types'),
                  'truncated':bool(s.get('truncated')),
                  'shapes':[{'fields':x.get('fields',[]),'other_fields':x.get('other_fields',0),
                             'count':x.get('count')} for x in s.get('shapes',[])]}}
print(json.dumps(out,separators=(',',':'),sort_keys=True))
PY
else
  echo '{"status":"no_snapshot"}'
fi
exit "$code"
'@

& $ssh -i $key -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$knownHosts" -- 'opc@140.238.71.236' $remote
if ($LASTEXITCODE -ne 0) { throw "bounded alliance diagnostic exited with code $LASTEXITCODE" }
