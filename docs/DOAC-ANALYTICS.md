# Read-only DoAC analytics

Status: automated fake-server/fixture verification; **not live-tested in this session**. Work is on draft PR #1. No deployment or merge.

## Collect snapshots

Build with the Go version in `go.mod`. Use an existing private reconnect session configuration, kept outside the repository. Analytics refuses `-collect`, `-run-feature`, `-interactive` and account-binding `-email`. It does not auto-create guests. Existing non-analytics commands are preserved.

The separate `-member-thp-output` validation path is intentionally narrower than
general analytics. It supports either a private session extracted from a
server-accepted official Login or private email verification followed by a fresh
loginKey-based role connection. It requires the observed role to be server 1723 and
to have delivered `init`, and sends only `dragon.assign.player.info {}`. It
exclusively creates a mode-0600 JSON document outside the repository containing
only `uid`, `display_name` and `thp` records plus minimal provenance. Existing
files and symlinks are refused. Exactly 100 unique rows with non-empty identities
and valid THP are required for success; incomplete normalized replies remain
available for manual comparison and are never treated as complete membership.
The captured session contains an opaque encoded Login template as well as credentials;
keep it outside Git and recapture after a server redirect rather than attempting to
rewrite unknown UID/zone-bound fingerprint fields.

Copy `analytics.example.json` to a private location. Set the confirmed alliance ID and explicit game-day/week/season labels. Do not guess the alliance ID or use the computer's weekday as the game day. Output is append-only JSONL with mode 0600.

```bash
go build -o /tmp/lastwar-client ./cmd/lastwar-client
/tmp/lastwar-client -config /private/existing-session.json -analytics /private/analytics.json
```

In the separate [lastwar-dashboard repository](https://github.com/mysteryhawk/lastwar-dashboard), import those snapshots with `python3 tools/ingest_analytics.py --db /private/doac-history.sqlite3 /private/doac-snapshots.jsonl`. The Go client does not depend on Python. See [dashboard PR #1](https://github.com/mysteryhawk/lastwar-dashboard/pull/1) and its [version-1 contract](https://github.com/mysteryhawk/lastwar-dashboard/blob/feature/history-dashboard/docs/SNAPSHOT-CONTRACT.md).

This is the intended future VM workflow, not a deployment performed by this session. Run one collection per invocation; scheduling is deliberately separate from protocol correctness. Session expiry retains existing auth exit code 2; terminal pushes retain code 3; other failures exit 1. Each completed query exports its status before later queries proceed. Output/logs must stay private because member identity is analytics data.

Supported query kinds:

| Kind | Required options beyond `kind` and `period` | Defaults/meaning |
|---|---|---|
| `membership` | none | Explicit config alliance ID; `al.rank` |
| `thp` | none | `rank.get`, type 13, `heroPower`; global 0 (config can specify 1) |
| `vs_daily` | `day`: 1..6 | type 0; same explicit game-week label for days 1–6 |
| `vs_weekly` | none | type 1; no day |
| `vs_season` | none | type 2; no day |
| `desert_thp` | none | implicit logged-in alliance; Desert Storm `users[].heroPower`; no paging |
| `season_power` | `rank_type`: 2 or 4 | page_size 100; individual seasonal `score` |
| `season_devotes` | `event_id`, active-event `sub_type` | start 1,end -1; no guessed event ID; `score` |
| `svs` | none | Candidate cross-war personal ranking; confirm event applicability live |
| `meteorite` | active-event `grab_times` | start_rank 1,count 100; zero-based round index |

Read [DOAC-PROTOCOL-FINDINGS.md](DOAC-PROTOCOL-FINDINGS.md) for handlers and uncertainty. Paging/round/event dimensions are exported in `parameters`; do not combine variants merely because their metric names match. Ranking results may contain non-DoAC players; reconcile against the roster by UID. The snapshot's `alliance_id` is collection context, not proof each ranking row belongs to that alliance. Daily/weekly VS are explicitly aid-filtered.

`grab_times` and `season_devotes.sub_type` must be present even when their confirmed
active-event value is zero. Omission is rejected so an unknown round or subtype cannot
silently become a real wire value.

`desert_thp` is the candidate full-alliance THP source used by the Desert Storm
participant selector. It sends no alliance or server parameter, so use it only from an
authorised member session on the target server. Until a live response matches the observed
roster UID set, treat its coverage as unverified and keep `complete_roster=false`.

For six-day VS comparisons, give all six daily queries the same confirmed game-week `period`, with `day` 1–6. Repeated polls are snapshots, not additional scores. Missing days remain unknown. THP/roster may use explicit game-day labels.

## Snapshot contract

One JSON object per query, schema_version 1:

- UTC `collected_at`; observed `source_server` from bootstrap metadata, separate from configured `target_server`. Unknown source is 0. A guest on 1456 is never relabelled as 1723.
- `alliance_id`, command, metric, explicit period, optional daily day, and allowlisted request parameters.
- Status: ok, empty, partial, missing_payload, server_error, transport_error. Missing fields and malformed numbers never become fabricated zero scores.
- `validation=response_received` means the client received a correlated response. It does not certify DoAC access, complete ranking coverage or live validation by the developer.
- `payload_present`, `rejected_rows`, `complete_roster`; records contain UID strings, optional names/alliance/server, optional rank, and signed 64-bit integer values.

Only normalised allowlisted fields are exported. Raw init/login/server responses, session tokens and credentials are never exported. JSON numbers retain exact integer values in Python/SQLite; consumers in JavaScript must preserve 64-bit score precision themselves. UIDs always remain strings.

The parser supports SFS array/object-map collections, checks optional echoed query dimensions, rejects malformed/duplicate rows, recognises roster aliases, and never substitutes city power for THP. The strict `_id` dispatcher does not guess missing-ID responses from the command name; such responses are counted as unmatched and the request times out. This is deliberate until protocol evidence supports a safe alternate matching rule.

## SQLite and dashboard path

The importer uses Python's standard library, transactions and canonical snapshot hashes. Reimporting the same file adds nothing. An invalid line rolls back the entire batch. Schema:

- `analytics_snapshots`: query context, timestamps, parameters, statuses and completeness.
- `analytics_records`: each UID's name, value and rank within a snapshot.
- `analytics_observations`: joined view for Flask queries.
- `analytics_membership_events`: joined/departed/name_changed transitions between authoritative full rosters. First roster is a baseline; late imports rebuild events in timestamp order.

Go currently exports `complete_roster=false`. Before enabling this, confirm a nonempty `al.rank` response contains every member, with no hidden pagination or access filtering. Empty guests, partial payloads, source-server mismatches and ranking absence must never infer departures. All observations/name histories are already retained by UID.

The uploaded dashboard prototype has now been inspected: it is a manual name-keyed CSV app without THP, UID or collection dimensions. Its installed VM state and any real legacy data remain uninspected. A standalone Python/Flask replacement is committed in private `mysteryhawk/lastwar-dashboard`, branch `feature/history-dashboard`, draft PR #1. It reads this history schema, supports six-day VS coverage and configurable THP/activity groups, and passed nine synthetic dashboard tests plus five ingestion tests. See the current agent state for the preserved source package and handover. Keep the history DB separate from the legacy prototype DB; do not auto-map names to UIDs. Flask can open it read-only (`sqlite3.connect('file:/private/doac-history.sqlite3?mode=ro', uri=True)`) or attach it for reporting. Existing dashboard migration is not implemented or deployed.

Example history query (use bound parameters):

```sql
SELECT collected_at,period,day,parameters_json,value,rank,status
FROM analytics_observations
WHERE uid=? AND metric='THP' AND source_server=1723 AND target_server=1723
ORDER BY collected_at;
```

For an alliance table, join ranking UIDs to the appropriate roster snapshot before displaying membership. A server THP leaderboard may omit members below its cutoff. Treat them as missing observations. Never fill missing values with zero or infer a departure.

## Interactive protocol research

Interactive mode now continuously drains pushes with one dispatcher. Existing generic integer params remain Long for compatibility. Known Lua fields use Int automatically (VS day, rank.get type/global/serverId, season rank_type/pageSize, server.rank ismerge). Explicit scalar wrappers:

```text
al.battle.rank.info {"type":0,"day":1}
rank.get {"type":13,"global":0,"serverId":1723}
some.read.command {"field":{"$sfs":"int","value":1}}
```

Wrapper types: byte (0..255), short, int, long, double, bool, utf. Arrays/ordinary nested objects remain unsupported. Interactive itself is an existing unrestricted research interface; the dedicated analytics mode only accepts its read-only query constructors.

## Verify locally

```bash
go test ./...
go test ./... -race
go vet ./...
go build ./...
GOOS=linux GOARCH=arm64 go build -o /tmp/lastwar-client-arm64 ./cmd/lastwar-client
```

Transport tests cover >1000 idle pushes, out-of-order concurrent requests, stale replies, typed IDs, caller-ID rejection, timeouts/cancellation, terminal pushes, disconnects, complete corrupt packet recovery and bootstrap deadline clearing. Analytics tests cover Lua wire tags, parser strictness, echo mismatches, THP field choice, UID precision, filtering, aliases, credential-free export and private CLI output. Python tests run independently in the dashboard repository (`python3 -m unittest discover -s tools -p 'test_*.py' -v` and, with Flask installed, `python3 -m unittest discover -s dashboard -p 'test_*.py' -v`). Python tests cover transactional rollback, idempotency, exact int64 scores, safe membership events and out-of-order import.
