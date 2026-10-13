# DoAC agent state

## Standing operating authority (2026-10-14)

Repository-wide autonomous-development authority and safety boundaries are recorded in
[`../AGENTS.md`](../AGENTS.md). They supersede earlier requirements for individual
approval of routine SSH access, project-local toolchain work, temporary validation,
ordinary development commits and pushes. They do not authorise production deployment,
live-database mutation, persistent VM jobs, paid infrastructure, gameplay actions,
default-branch merges or destructive operations. On live authentication rejection,
stop further authentication attempts immediately and continue offline.

Updated: 2026-10-14. Read this alongside [DOAC-HANDOVER.md](DOAC-HANDOVER.md).

## Current state

- Branch `feature/interactive-continuous-reader`; draft PR #1. Main unchanged, no VM deployment, no game-server calls in this session.
- Committed transport milestone: `20667c2` (dispatcher, typed scalars, updated tests/state). Initial state: `bbfcd76`.
- Committed collector/storage milestone `874b4d7`: read-only VS daily/weekly/season, THP type 13, roster, season personal power/devotes, candidate SvS and meteorite queries; strict parsers; private JSONL CLI; transactional SQLite importer.
- Automated verification: original full suite and transport/app/SFS race suite passed; latest collector/CLI race tests and five Python importer tests passed. Whole-repository `go test ./... -race`, `go vet ./...`, native build, Linux ARM64 build and gofmt checks passed. Python ingestion/dashboard tests now have independent CI in the dashboard repo (the original client copy is being removed in this milestone).
- Live verification: none here. Historical upstream roster evidence remains clearly labelled. Full-roster marking deliberately disabled until live completeness/access semantics are checked.
- Blocked: this Work runtime cannot reach the supplied Oracle VM SSH endpoint (TCP connection fails locally with `OSError: [Errno 101] Network is unreachable`); target-server/alliance live session, actual dashboard schema and active event IDs/rounds. THP constant is no longer blocked: recovered from the public historical Lua bundle.
- Independent dashboard published: private `mysteryhawk/lastwar-dashboard`, branch `feature/history-dashboard`, draft PR #1, implementation `795117a`. Python importer/tests now live there; this milestone removes the client copy.
- Immediate next action: inspect live VM/session from an SSH-capable environment, verify DoAC/server1723 payloads and coverage, then browser/container QA of the dashboard. Both PRs remain draft; no deployment or merge.

## Architecture and environment

Go module `lastwar-client`, Go 1.27.2, klauspost/compress 1.20.1. `internal/session/conn.go` owns packet reads, serialised writes, extension ID sequence, heartbeat and terminal-session handling. `wire.go` provides synchronous command waits. Login and existing game features depend on these synchronous reads; preserve them before entering analytics/interactive mode. `internal/app/interactive.go` owns FIFO parsing and process shutdown. `internal/sfs` implements typed SFS2X objects/framing. Existing game features include gameplay-changing requests; analytics must never invoke them.

Oracle Linux ARM VM paths from handover: `/home/opc/lastwar-client`, `/home/opc/lastwar-lua`, Flask/SQLite `/home/opc/doac-dashboard`. Neither VM deployment nor its dashboard schema has been inspected in this session.

## Confirmed findings and decisions

- PR head at recovery: `7a4ac230094a86b23b60decd4bf0a475b016fb9f`; base main `312071b9b561fbc06e80b411b7d3e1bd228fd63c`.
- Draft changes `handleInteractiveLine` signature without updating existing callers in tests. Initial reader queues all extension messages (256 slots), drops overflow and matches by command only. It is not verified and will be replaced.
- Register pending requests before sending. Require echoed Int `_id` and expected command (including `push.` alias); never fall back to command-only matching. Missing IDs are pushes, not evidence of command failure.
- Socket deadlines must be cleared before continuous reading. Request timeouts must not interrupt socket framing: retrying packet reads after a partial-frame timeout can desynchronise the stream.
- Repository `docs/live-validation.mdx` documents upstream live evidence (2026-10-06): `al.rank {allianceId: Utf}` returns `list[]` roster. This is stronger evidence than the old empty no-parameter guest request. It is not live verification by this session.
- VS types/field tags and rankInfo fields are historical Lua findings in the handover. THP is `heroPower`, distinct from `power`; numeric HERO_TOTAL_POWER was unknown at recovery; it is now confirmed by static Lua analysis as 13. See the protocol findings document.

## Validation and experiments

- `git clone`, checkout existing branch and inspect log/status succeeded. Clean checkout at recovery.
- `go version` failed: Go not installed on PATH. Toolchain acquisition in progress; no tests claimed as passing yet.
- Personal Context retrieval found no additional THP constant or SQLite schema. Do not repeat broad conversation retrieval.

## Initial recovery priorities (completed; retained for context)

1. Implement dispatcher and fake-server tests: >1000 idle pushes, ID/command mismatch, stale replies, deadlines, cancellation, disconnection, terminal push and clean shutdown. Integrate FIFO and update existing tests.
2. Implement explicit scalar types and Lua-derived typed VS builders, strict UID-keyed parsers, roster query and configurable rankings without guessed constants.
3. Add timestamped JSONL snapshots and transactional SQLite ingestion; missing/partial rankings must not infer departures.
4. Run full tests, race tests, vet and ARM build; commit each milestone and update PR.
5. Live verification requires authorised existing game session and target-server/alliance access, plus VM-only Lua definitions for THP/season variants. No deployment or merge authorised.

## Repository and handover discipline

This file is the living record. Original handover stays relatively stable. Each milestone must commit code/tests and updated state together. Distinguish automated verification, upstream historical live observations, this session's live tests (none), and implementation awaiting live validation. Never store private captures, credentials or real member data in GitHub.

## Milestone 1 — transport and typed parameters

`internal/session/dispatcher.go`: idempotent `GameConn.StartDispatcher`, sole continuous reader, 128 pending requests maximum, registration before send, command plus Int ID correlation, context deadlines/cancellation, stale/unmatched counters and error propagation. Unmatched envelopes are discarded without queueing. Bootstrap and existing synchronous APIs remain intact; do not call them after starting dispatcher. Complete-frame object decode failures are safely skipped (`EnvelopeDecodeError`); packet/read failures terminate the dispatcher, including read timeouts, to avoid partial-frame desynchronisation. `Close` waits for reader shutdown.

`interactive.go` uses the dispatcher and preserves original handler signature. Existing fake replies now echo the incoming ID. `sfs.JSONScalar` supports explicit `{"$sfs":"int","value":1}` wrappers (byte/short/int/long/double/bool/utf) with range checks and no credential values in errors. Known Lua command numeric fields use Int automatically; generic integers remain Long for compatibility.

Go 1.27.2 obtained from official go.dev into scratch `/tmp/doac-toolchain`; no dependency change. Initial full test failed with draft handler signature errors; corrected implementation full tests passed. New net.Pipe tests passed for 2001 idle pushes, wrong command/ID type, push alias, concurrent out-of-order matching, stale timeout reply, pre-cancelled contexts, disconnect/terminal push, corrupt complete packet, deadline reset and caller-ID rejection. Race run in progress at this milestone.

Git CLI clone/fetch works but HTTPS push lacks credentials. Use connected GitHub create_tree/create_commit/update_ref with expected head; do not request PATs. Initial state committed remotely as `bbfcd76`. Reconcile local branch with fetch/reset-soft after connector commits.

## Milestone 2 — analytics and historical storage

Architecture: `internal/analytics/types.go` owns typed read-only query descriptors and normalised snapshots; `config.go` loads strictly validated config and builds allowed queries; `parse.go` parses SFS arrays/maps with identity/number validation and optional echo checks; `collector.go` uses the Requester interface and context-bounded requests; `internal/app/analytics.go` exports private JSONL using an existing connection. `-analytics` rejects gameplay/account-binding/interactive flags and fresh guest creation; cross-server bootstrap determines observed source server. `tools/ingest_analytics.py` transactionally imports immutable observations into a separate SQLite history DB; membership transitions require explicitly authoritative snapshots.

Confirmed static discovery: official LWLF 826 archive downloaded and decompiled locally. THP type=13, rank.get Int fields and serverRanking/heroPower confirmed. Roster alias userId/playerName, season personal types 2/4 and payload aliases, devotes event/day/range parameters, meteorite data list and zero-based grabTimes, and candidate SvS ranks list recovered. Full provenance and reproducible extraction instructions: [DOAC-PROTOCOL-FINDINGS.md](DOAC-PROTOCOL-FINDINGS.md). User workflow and SQLite contract: [DOAC-ANALYTICS.md](DOAC-ANALYTICS.md).

Decisions: UID stays a string; scores stay int64, never float. Parameter dimensions and explicit period labels prevent mixing rounds/season variants. Snapshot collection context is not proof every rank row is a DoAC member. Incomplete/missing rankings never infer departure. Source server is observed metadata, not target substitution. No speculative season.contribution.info parser: dispatch entry exists but its handler is absent in the 826 bundle. No speculative all-devotes/faction-score parser where response structure remains unknown. Ranking completeness, global scope and live score semantics remain unverified.

Transport refinements: cap in-flight writer goroutines as well as pending requests; exhausted request IDs require reconnect. A framing-level read timeout is terminal, unlike a request context deadline. Updated the old synthetic timeout test to exercise a real request deadline on a live net.Pipe reader; repeated targeted tests passed. Existing complete packet decode errors remain skippable.

Experiments: baseline draft failed compile (handler signature); corrected full suite passed. Collector/CLI fake-server race tests passed. Python importer initially failed due to SQL placeholder count; fixed, all five tests passed including rollback/idempotency/membership transitions. Whole-repo tests caught existing source-scan assumptions (new explicit conn.Close exit, classification of leaderboard global field); updated those checks without weakening their invariants. Latest full suite passed after corrections, before final added query-dimension metadata; final whole-repository race suite passed, as did vet, native build, ARM64 build, formatting and diff checks.

Failed setup experiments to avoid repeating: apt-get download JDK failed because package index was unavailable; use JDK when installed or ECJ Java compiler. Web retrieval tool could not open the binary Lua archive; direct HTTPS download succeeded. No need to request VM-only Lua files for these findings.

Remaining priorities:
1. Completed: full race/build/vet/ARM checks; implementation/tests/docs committed as `874b4d7`; PR rewritten. Proceed to live validation when environment access is available.
2. In an authorised existing VM/session environment, collect redacted payload schemas for VS/THP/roster; measure idle Recv-Q and compare roster UIDs against THP coverage. Do not create a guest automatically or mislabel server 1456 as 1723.
3. Confirm roster is complete before enabling complete_roster; then verify membership events from successive real snapshots.
4. Identify active season event ID/subtype, meteorite round and SvS event applicability; validate builders against live replies. Unknown structures remain missing implementation/evidence, not claimed unavailable.
5. Read the existing Flask/SQLite schema and adapt its queries to the new history DB. No live DB migration or deployment authorised.
6. Confirm leaderboard truncation and determine a documented read-only per-member THP source if ranking coverage is incomplete. No paging params occur in rank.get's historical handler.

Final safety review refinement: writer errors now terminate the dispatcher even after the caller cancelled; top-level params are copied before asynchronous send. No live data/captures/authentication material created or committed. Source records remain private, with ignored JSONL/SQLite outputs.

Final ownership review: `ReadEnvelope` now rejects callers after dispatcher ownership starts (`ErrReaderOwned`); bootstrap reads and ownership transfer are serialised. This prevents accidental competing readers instead of relying only on documentation. Normal-login analytics branch removed as unreachable because analytics requires reconnect configuration.

## End-of-session verification and repository state

- Final implementation head: `874b4d7a7c43a326ef99e050f3cb6384f2752d56`. Transport milestone: `20667c2caecde0020c86edbbbc1cff00299a8653`. State recovery: `bbfcd76e02ac962bc8d97f97078fc5f6f48ea2f0`. This final handover update follows the implementation head.
- PR #1 title: "Add correlated read-only DoAC analytics collector and SQLite history exports". Open, draft, no merge/deployment. Main remains `312071b9b561fbc06e80b411b7d3e1bd228fd63c`.
- Final ownership-change whole-repository `go test ./... -race` passed: app ~76s, game ~66s, SFS ~36s; all packages passed. Dispatcher tests also repeated three times under race detector. `go vet ./...`, native build, Linux ARM64 build, gofmt and `git diff --check` passed. Five Python importer tests passed. CLI help exposes analytics flag correctly. No live game-server test occurred.
- GitHub connector reported no workflow runs or status checks for the pushed implementation head at inspection. **Do not claim remote CI passed**; local checks are the verification evidence. Python ingestion is included in the workflow for future CI runs.
- Fetch after connector push reconciled local checkout with remote, with no outstanding implementation changes. Final agent-state update is committed separately to leave a precise restart point.
- Files added: `internal/analytics/{types,config,parse,collector,analytics_test}.go`, `internal/app/analytics{,_test}.go`, `internal/session/dispatcher{,_test}.go`, `internal/sfs/json_scalar{,_test}.go`, analytics key classification test, `tools/{ingest_analytics,test_ingest_analytics,extract_lua}.py`, `analytics.example.json`, usage/protocol/state docs. Initial queue file deleted; existing interactive/main/conn tests and CI/gitignore updated. Original handover preserved.

Access needed next: an authorised execution environment for the existing Oracle VM/session, or equivalent local authenticated session environment, to issue read-only ranking requests and inspect bootstrap source-server metadata; read access to `/home/opc/doac-dashboard` source/schema for integration. No private keys/session tokens should be put in repository or conversation. Do not deploy, merge or alter live game state. With no access currently available, remaining live validation and actual Flask integration cannot be truthfully completed.

## Follow-up — VM connectivity blocker (2026-10-11)

The user supplied the Oracle VM address and SSH port 22. A non-authenticated Python `socket.create_connection((host, 22), timeout=10)` failed immediately with `OSError: [Errno 101] Network is unreachable`, before an SSH banner or authentication exchange. This confirms a routing restriction in this Work execution environment; it does not establish that the VM is down or its firewall is blocking SSH. No private key was requested, generated or used. No VM or game connection was established. Available connectors expose no suitable remote shell.

Next action: resume live inspection in an authorised SSH-capable execution environment. A public IP or additional authentication material alone cannot fix the current runtime routing restriction; do not request private keys or game credentials in chat. An independently authorised remote execution facility would also suffice, but none has been established. Meanwhile, sanitised Flask source and schema-only SQL supplied through GitHub or files would allow offline integration development; exclude configuration secrets, SQLite data and account/session material. Do not repeat credential setup before checking network reachability in the new environment. Implementation and automated test conclusions above are unchanged.

## Follow-up — uploaded prototype and independent dashboard (2026-10-11)

The user supplied `doac-dashboard.zip` (SHA256
`6b842efd555cb544dff0f795845db3be65258a9f82ab2ac0bdb5ceea4fbc1c2a`) and confirmed it is essentially a prototype. The archive contains app.py, README.md, Containerfile and compose.yaml, no DB or credentials. Its schema is `players(id,name UNIQUE,active)` and `scores(player_id,day,event,score)`; it supports manual CSV entry and rolling seven-day summaries, lacks game UID/THP/timestamps/event dimensions. This is source evidence, not an inspection of the installed VM or actual stored data. No need to request the schema-only command for this same prototype design.

User requested a separate `lastwar-dashboard` GitHub repository so Python and Go can develop independently. Prepared a standalone project locally, keeping all dashboard additions out of the client branch. New repo publication is blocked because the connector has no create-repository operation. User must initialise private `mysteryhawk/lastwar-dashboard` with a README and ensure connector access; then publish on a development branch/draft PR without merging. The already-committed importer remains in this client temporarily until independent publication; do not delete its working implementation prematurely.

Prepared deliverable: `lastwar-dashboard-prepared.zip`, preserved as a new downloadable file alongside the user's original upload. Package SHA256 `9cc4215dae7c18a8a3b8b275411aec4c610ad444464601bb0d1b8a481ae747f7`. It contains all dashboard source, importer/tests, independent Python CI, contract, README and its own `docs/DOAC-AGENT-STATE.md`. It contains no DB/data/credentials. If local scratch has expired, retrieve this exact prepared package, not the original manual prototype. Original uploaded file was preserved.

Architecture: `dashboard/history.py` provides mode=ro/query_only SQLite projections, historical alliance/UID scope, latest collection summaries, per-UID timestamped trends and six-day VS aggregation. `dashboard/app.py` provides authenticated Flask pages/API/CSV, CSRF POST checks, private responses and an application factory. `dashboard/templates/` contains history/login/threshold-group views. `tools/ingest_analytics.py` copied to the independent project. `docs/SNAPSHOT-CONTRACT.md` records version-1 JSONL semantics. No Go runtime dependency in dashboard.

Features tested locally: read-only/no-create DB access; unavailable DB; latest failed collection suppressing stale scores; source-server mismatch and wrong alliance/player-server exclusion; no future-roster use for past rankings; exact large UID/value; variant/week separation; repeated day polling; six-day coverage; renames/trend gaps; all four THP/activity threshold groups with explicit thresholds and an insufficient-data group; authenticated routes, CSRF/Unicode tokens/logout, HTML escaping and formula-safe CSV. Nine dashboard tests and five importer tests passed from the standalone root. Flask installed into scratch venv, original prototype dependency pins reused. New comparison template test caught a malformed brace; corrected and suite passed. No browser visual QA, container build, live server validation or deployment claimed.

Updated client analytics example to request all six VS days under one explicit game-week label. Days remain distinct exported dimensions. Dashboard classification only uses six observed values, never zero-fills missing players/days. Weekly game ranking remains separate. THP and VS periods are selected explicitly; groups describe observations, not current membership. CSV prefixes UIDs to preserve spreadsheet precision; JSONL/API is the machine interface.

Next sequence:
1. Publish prepared dashboard package to the new repo and record actual commit/PR URLs in both states. Verify independent Python CI when available.
2. Link the published dashboard contract/importer from client docs; retire client importer copy only after independent publication is safe.
3. Resume read-only VM inspection from Codex CLI or another SSH-capable environment. Inspect any real legacy data before migration; never auto-map names to UID.
4. Live-verify DoAC/server1723 access, THP coverage and roster completeness, then season/SvS/meteorite event semantics.
5. Browser/mobile QA, container build and deployment remain outstanding. No deployment or merge authorised.

This milestone changes client documentation and example config only; Go implementation and its previous race/build/vet verification are unchanged. Client working tree will be reconciled after committing this state.

## Repository separation completed

User created private `mysteryhawk/lastwar-dashboard`. Connector confirmed push access.
Implementation is committed as `795117a4ac5883a57e0c758f77efc9a3e23c7b85` on
`feature/history-dashboard`; initial state `ef712e48f7f57a7c241f9e2ee0f006e68cf33321`.
Draft PR: https://github.com/mysteryhawk/lastwar-dashboard/pull/1. Main baseline
`f58259ed71670dcf575b6149a1d11acfad9fa859` remains unchanged.

The earlier repository-creation blocker is resolved. GitHub is now the authoritative
dashboard source; the earlier prepared ZIP is a bootstrap checkpoint, not the latest
state. Do not repeat repository creation or recreate dashboard code from the old upload.

This client milestone removes `tools/ingest_analytics.py` and its Python tests/CI step,
and links the independent dashboard importer/contract instead. Lua extraction remains
in the client. JSONL schema version 1 and Go implementation are unchanged. The analytics
example now includes six VS daily queries sharing an explicit week label. Final local
checks: standalone nine dashboard tests and five importer tests passed; Python compilation
and client git diff --check passed. Previous Go race/vet/native/ARM checks remain applicable
because no Go code changed. Remote dashboard CI status is inspected separately, not assumed.

Next: read the dashboard's own docs/DOAC-AGENT-STATE.md and docs/SNAPSHOT-CONTRACT.md;
continue live verification using an SSH-capable environment. Current Work network limitation
is unchanged. No Oracle changes, credentials, live game calls, main merges or real member
data were involved.

## Independent dashboard verification confirmed

Dashboard GitHub Python CI passed for `795117a` (implementation) and `16a3c73`
(handover), with runs 38100074657 and 38100173930 respectively. Its local Gunicorn
factory --check-config, real localhost /health and protected-page redirect passed.
Final dashboard verification state: `e98bb93a0e22133d603841d4c7357793ab8d5756`.
Draft PR https://github.com/mysteryhawk/lastwar-dashboard/pull/1 includes these results.
No browser/container/VM or live metric verification was performed.

Client PR #1 title is now "Add correlated read-only DoAC analytics collector and JSONL exports".
Client separation commit `b83a3641a4af9e861e17204ef6b7604e01e20374` is reconciled locally.
The example config was checked as valid JSON with distinct days 1–6 under one week label.
This final handover-only update follows it. No uncommitted implementation remains.

## Checkpoint — read-only VM and fresh-guest live verification (2026-10-11)

SSH to the authorised Oracle VM succeeded. Read-only inspection found no running
`lastwar-client`, tmux/screen session, user service or Podman container. The historical
PID/session is gone. `/home/opc/lastwar-client` remains clean on `main` at `312071b`;
the feature branch has not been deployed. `/home/opc/doac-dashboard` is still the
manual prototype with `data/doac.sqlite3`; neither it nor the database was changed.

The returning-guest distinction in the original handover was reproduced. The existing
persisted guest resolved through GSL to source server 1972 and was rejected during
Login with `ec=28/E011`, before any extension request. This is expected returning-guest
authentication behaviour, not evidence of a protocol or target-server failure.

Three isolated fresh guests authenticated on their first connection, on observed source
servers 1241, 95 and 1262. Temporary guest state and private logs were removed after each
probe. From a source-1262 session, `rank.get` with type 13/global 0/serverId 1723 returned
100 THP rows. The response contained 11 case-insensitive DoAC markers and the row schema
near them included `uid`, `heroPower`, `srcServer`, `allianceId`, `abbr` and
`alliancename`. This is confirmed live evidence that a guest on another source server can
read the server-1723 THP ranking; it does not establish complete alliance coverage.

`al.battle.rank.info` returned success-shaped responses containing `rankInfo` but no
`uid`/`score` rows for weekly type 1, daily type 0 day 1, and season type 2 across the
fresh probes. Treat these as empty/access/event-context observations, not transport
failures or proof that VS is unavailable. No roster, complete-roster semantics,
season/SvS/meteorite request, gameplay-changing request, deployment or database write
occurred.

Checkpoint verification: both local worktrees were initially clean and matched their
remote feature branches. Pull-request refs matched client `62cec50` and dashboard
`e98bb93`; both PRs remain documented as draft. Dashboard importer tests passed (5).
Dashboard Flask tests could not run in this shell because Flask is not installed. Go is
also absent from local PATH, so previous full Go verification remains the latest automated
evidence. These are environment limitations, not test failures in the implementation.

Exact next action: in one fresh first-login guest session, privately extract the consistent
`allianceId` from the DoAC THP rows, call read-only `al.rank` with that ID, and compare
roster UIDs with the 100 returned THP UIDs. Record counts/coverage only and keep
`complete_roster=false` until completeness is demonstrated. Then investigate why VS
rankings are empty before moving to active season/SvS/meteorite identifiers and rounds.

## Milestone — roster coverage, live-shape tests and identifier privacy (2026-10-11)

The exact next action above was completed without deploying the feature branch. A fresh
guest on source server 477 retrieved 100 server-1723 THP rows; 11 were DoAC rows with 11
unique UIDs. The privately derived single DoAC alliance ID was then sent to read-only
`al.rank`. It returned 100 unique roster UIDs, all explicitly on server 1723. All 11 THP
DoAC UIDs overlapped the roster and 89 roster UIDs had no THP row. A second fresh session
on source server 439 repeated `al.rank` twice: both responses contained the same 100 UID
set. No useful independent member-count field was present, so a possible 100-row cap
remains unresolved and `complete_roster` stays false.

Client changes add synthetic regressions for the observed `allianceId`/`srcServer` THP
shape and success-shaped empty array/object VS payloads. `gameUid` is now treated as a
stable private account identifier: SFS redaction and all identified production log/error
sites mask it. This closes the cleartext identifier exposure observed during the returning-
guest inspection; no real identifier is stored in tests or Git.

Automated verification: Go 1.27.2 was downloaded from the official Go distribution.
`internal/analytics` and `internal/sfs` tests passed. Linux auth/app test binaries compiled
and Linux-targeted `go vet ./...` passed. Direct Windows execution of auth/app tests is
not supported because existing tests use Unix `syscall.Mkfifo`; full Linux execution is
the next verification step. No feature build was run on or deployed to the VM.

Immediate next action: run the exact committed branch's full Go race suite in an isolated
temporary VM checkout after approval, without game access or replacing the installed
client. Then investigate VS context/period fields using aggregate-only diagnostics during
an active event or with an authorised participating session. Do not enable roster authority.

## Checkpoint — pushed and Linux-verified (2026-10-11)

Commit `fc39d76` (`analytics: record live roster evidence safely`) is pushed to
`feature/interactive-continuous-reader`. The approved Linux check used a credential-free
archive of that exact commit in a new VM `/tmp` directory. The VM's installed Go 1.26.7
was too old for `go.mod`, so the official Go 1.27.2 Linux ARM64 archive was checksum-
verified against go.dev and unpacked only inside the temporary directory. `go test ./...
-race`, `go vet ./...` and `go build ./cmd/lastwar-client` then completed successfully.
Explicit post-run cleanup removed the source, toolchain and transfer archives after the
initial trap did not do so. This is automated Linux
verification only: no feature binary was deployed and no game request was made.

Current blocker and exact next action: daily/weekly/season VS responses remain live-
observed but success-shaped empty for fresh non-participating guests. Obtain an authorised
participating session during an active VS period (or wait for the event to be active), then
run aggregate-only read-only VS queries for days 1-6, weekly and season. Record response
keys, row counts and period identifiers without raw member data. Do not treat emptiness as
zero, enable roster authority, deploy, or query season/SvS/meteorite until applicability is
established.

## Milestone — VS context gates and explicit event dimensions (2026-10-11)

Version-matched Lua on the VM was inspected read-only. The daily VS tab is hidden when
the alliance-duel activity is absent, finished or has no event info; the season tab is
hidden unless the account is in a league match, while weekly remains visible. Once a tab
is selected the UI sends `al.battle.rank.info` directly, with no further client-side
participation guard. This strengthens the interpretation of fresh-guest empty payloads as
event/account-context evidence rather than a protocol implementation failure. The wire
query has no week dimension; an observed duel `weekEndTime` should anchor the explicit
snapshot period label.

Configuration now rejects omitted meteorite `grab_times` and season-contribution
`sub_type`. Both zero values are legitimate wire dimensions, so absence must not silently
select zero and masquerade as a confirmed active round/subtype. Explicit zero remains
accepted. Focused analytics/SFS tests and Linux-targeted `go vet ./...` passed; a full
Linux ARM64 race-suite run against the exact commit remains the next automated check.

Immediate live next action remains unchanged: use an authorised participating session
during an active VS period, preflight the read-only duel event context, derive the period
from `weekEndTime`, then query days 1-6, weekly and season. Stop if applicability is not
established; do not guess event identifiers or rounds.

## Checkpoint — event-dimension milestone verified (2026-10-11)

The implementation milestone is pushed as `92520b2`; two follow-up test/privacy fixes are
`58c4b05` and `ecf9f88`. The first parallel Linux ARM64 race run exposed three pre-existing
or newly visible checks: two GSL source-regex tests assumed LF-only source, and one slow app
test missed its expected malformed-push warning under package-level resource contention.
The GSL checks now accept LF and CRLF and pass locally. The app test passed alone under the
race detector. A subsequent sequential full run exposed the credential-leak lint rejecting
three `.String()` conversions directly inside log sinks even though their values were
wrapped by `sfs.Redact`; conversion/redaction now happens before logging.

Automated verification of exact commit `ecf9f88` on Linux ARM64 with checksum-verified Go
1.27.2: `go test -p 1 ./... -race`, `go vet ./...` and
`go build ./cmd/lastwar-client` all passed. Temporary source/toolchain directories were
removed. This was isolated automated verification, not a deployment or live game test.

Confirmed live/static boundary: THP and observed roster access remain live-verified;
VS empty payloads remain live observations from fresh non-participating guests; the daily
and season UI context gates are static Lua evidence. No participating session or active
event identifier is available. Exact next action is therefore still the authorised active-
VS preflight and aggregate-only days 1-6/weekly/season capture described above. After that,
test season power type 2 before type 4; only query devotes, meteorite or candidate SvS when
their event ID/subtype, round or applicability is observed rather than defaulted or guessed.

## Milestone — Desert Storm full-alliance THP path (2026-10-12)

Version-matched Lua establishes a better THP source than the capped warzone ranking.
The Desert Storm participant selector sends read-only `dragon.assign.player.info {}` and
handles an unpaged `users` collection. Rows are stored by stable `uid` and parse separate
`heroPower`, city `power` and `armyPower` fields. The Desert controller explicitly selects
hero power for display; the shared row component reads `heroPower`. Canyon/DSB's analogous
`dsb.act.player.list {}` parser lacks `heroPower`, so it is not used for THP.

The collector now supports `kind: desert_thp`, exporting only UID/name and `heroPower` from
`users`. Synthetic regression coverage proves city/army power cannot substitute. The
request has no alliance, server or paging parameter and is therefore implicitly scoped to
the logged-in alliance. It must be run from an authorised DoAC member session on server
1723; guest/cross-server results cannot establish DoAC coverage. `complete_roster` remains
false until returned UIDs match the separately observed roster.

Automated verification so far: focused analytics/SFS tests and Linux-targeted vet passed.
Exact next action: run the committed branch's full isolated Linux race suite, then use an
authorised server-1723 DoAC member session to collect `al.rank` and `desert_thp` in the same
session. Accept full THP coverage only if the UID sets match; keep missing rows unknown.

Checkpoint verification: exact commit `e0d475f` passed `go test -p 1 ./... -race`,
`go vet ./...` and `go build ./cmd/lastwar-client` on Linux ARM64 with checksum-verified
Go 1.27.2 in an isolated temporary directory. No binary was deployed and no game request
was sent. The only available persisted VM states are the previously inspected returning
guest contexts, not an authorised DoAC/server-1723 member session. Reusing or creating a
guest cannot validate this implicit own-alliance endpoint. Exact next action is therefore
to provide/use an authorised DoAC member session privately, then compare same-session
`al.rank` and `desert_thp` UID sets and record aggregate counts only.

## Milestone — guarded private member-THP probe (2026-10-12)

The client now has a narrowly scoped `-member-thp-output` flow for the user's
authorised DoAC account. It requires a fresh email verification in an isolated
state directory, confirms that loginKey and gameUid were durably persisted, closes
the initial guest-context socket, and reconnects. The second connection must have
used that loginKey, resolve the same verified gameUid, receive `init`, and observe
source server 1723 before any analytics request is sent. This prevents a fresh or
wrong-server guest from being mistaken for an authenticated DoAC role.

The probe sends exactly one read-only `dragon.assign.player.info {}` request. It
exclusively creates a mode-0600 JSON file in an absolute private path whose parent
is not group/world-accessible on POSIX. Existing files and symlinks are refused.
The document contains only collection time, observed server, command, response and
coverage status, a manual-scope-validation marker, and records containing stable
UID, display name and THP. Success requires exactly 100 unique non-empty UIDs and
names with non-negative `heroPower`; city/army power cannot substitute. Incomplete
normalised replies are retained with `status: incomplete`, never relabelled as
complete or zero-filled. Raw protocol responses and credentials are not exported.

Automated verification of the exact working source on Linux ARM64 with checksum-
verified Go 1.27.2: `go test -p 1 ./... -race`, `go vet ./...` and
`go build ./cmd/lastwar-client` passed. The first full race run exposed two unrelated
timing-sensitive interactive tests; both then passed three consecutive focused race
runs and the subsequent complete race suite. Local Windows production build,
Linux/ARM test-binary compilation and vet also passed; native Windows execution of
the existing FIFO tests remains unsupported. Temporary VM source/toolchain archives
were removed. No binary was deployed, no email verification occurred and no game
request was sent.

Static Canyon evidence remains incomplete. The known `dsb.act.player.list {}` row
parser lacks `heroPower`, despite the user's live observation that the Canyon
participant selector displays THP to an ordinary member. The exact Canyon cache or
secondary request sequence still needs capture/static tracing; do not substitute
`power` or `armyPower`.

Exact next action: commit and push this guarded probe, then—only with approval to
execute the reviewed build—run it interactively with the user's secondary DoAC
account. The user enters email and six-digit code privately; save the private
normalised file outside Git and compare its 100 UID/name/THP rows with the in-game
Canyon/Desert list. Treat alliance scope as manually unverified until that comparison;
do not deploy, ingest the file, enable roster authority or alter game/database state.

Checkpoint: guarded probe implementation and tests are committed and pushed as
`8e286cd` on `feature/interactive-continuous-reader`; draft PR #1 now describes the
current live/automated boundary. The exact committed source is the source that passed
the final Linux ARM64 race/vet/build run. Dashboard state checkpoint `226eddd` is
pushed separately. Both worktrees were clean after PR updates.

Immediate next action is now the private live run, not further implementation: obtain
explicit approval to execute the reviewed `8e286cd` build without installing it,
create a fresh mode-0700 account-state directory and private FIFO/output directory,
and let the user enter the account email and verification code in their terminal.
Retain the resulting normalised file privately for their visual comparison. If the
post-verification login fails, the observed server is not 1723, `init` is absent, or
coverage is not exactly 100 valid rows, stop and preserve the explicit incomplete/
failure evidence; do not fall back to a guest or another ranking.

## Live checkpoint — verified role reconnect rejected before THP (2026-10-12)

The user ran the reviewed one-shot wrapper from a private mobile SSH session. Email
verification and `account.login.new` succeeded, and the new identity durably received
a loginKey and the expected account role. GSL `opt=login` then returned code 0, one
role on `lastLoggedServer=1723`, zone `APS1723`, and a non-empty access token. The
subsequent SFS role Login was rejected with `ec=28`, `ep=[E011]`. The fail-closed
guard worked: `dragon.assign.player.info` was not sent, no member output was created,
and no game or database state was changed. This is live authentication evidence, not
a THP endpoint result.

Code reconciliation found that ordinary `Login` still sent the guest wire shape for
this second connection: both envelope `un` and `p.gameUid` were blank even though GSL
had selected the verified role. The established role-login implementation sends the
selected UID in both fields; it also derives `SecurityCode` from that UID and omits
five guest-registration fields. The branch now applies that role shape only to
GSL `opt=login`, preserves blank identifiers for `opt=new` guests, fails closed if a
loginKey resolves without a role UID, and returns exit code 2 for a game-server auth
rejection in the member-probe path.

Automated-only verification of the correction: focused `internal/auth` and
`internal/app` tests, the complete sequential race suite, vet and the CLI build
passed on Linux ARM64 in an isolated
`/tmp` source copy. The VM system compiler was Go 1.26.7, so only that temporary
copy's Go declaration was lowered; the repository's Go 1.27.2 declaration was not
changed. A regression test covers role UID selection, UID-derived security code and
absence of guest-only fields. No corrected binary has been executed against the game.

Exact next action: commit and push the correction and both state checkpoints, build a
reviewable corrected Linux ARM64 binary, then prepare a new empty mode-0700 state
directory. With the user's approval, repeat fresh email verification once and inspect
only aggregate outcome first. Do not reuse the failed directory for a supposedly fresh
probe, do not retry E011 blindly, and do not issue THP unless role Login, matching UID,
`init`, and observed source server 1723 all succeed.

Preparation checkpoint: the corrected `2028c81` Linux ARM64 binary is staged only
at `/tmp/doac-member-thp-2028c81` (SHA-256
`064fa7eea47c156002760a884ef879ff07466747897cb4bf2b8263ce39a3b250`) with mode
0700. `/tmp/doac-run-member-thp-rolefix` is the mode-0700 mobile wrapper. Its new
private directory `/home/opc/doac-private/probe-rolefix-2028c81` and `account-state`
subdirectory are both mode 0700, empty, and have no output file. The earlier failed
state is preserved separately. No corrected process has run. Exact next action is for
the user to execute `/tmp/doac-run-member-thp-rolefix` once from mobile SSH, enter the
email/code privately, and report only the aggregate status line or authentication
error; do not paste member rows or credentials into chat.

## VM toolchain baseline — permanent Go 1.27.2 (2026-10-12)

The Oracle Linux ARM64 VM now has the project's exact Go toolchain installed
permanently for `opc` at `/home/opc/.local/lib/go1.27.2`. Owner-controlled symlinks
`~/.local/bin/go` and `~/.local/bin/gofmt` take precedence over Oracle's retained
`/usr/bin/go` 1.26.7 through the existing shell PATH; no system package or profile was
replaced. The official `go1.27.2.linux-arm64.tar.gz` download was verified against
Go's published SHA-256
`94f3e30b8e374bc285e7dadc11e0865726b9bc6e85b841ccceaabc0214c6b7c8`
before installation, and its temporary download directory was removed.

A fresh login shell resolves both tools under `~/.local/bin` and reports
`go version go1.27.2 linux/arm64`; `GOROOT` is the versioned permanent directory and
`GOTOOLCHAIN=auto`. Against the unchanged Go 1.27.2 project declaration, focused
auth/app tests, vet and the client build pass. Future VM verification should use plain
`go` from the `opc` login environment; do not create temporary toolchains or lower
`go.mod` unless this installation is independently found damaged.

## Live checkpoint — known own-device E005 reproduced (2026-10-12)

A later fresh isolated run received and accepted a new email code. The initial guest
session succeeded, `account.login.new` returned the authorised role, GSL `opt=login`
returned code 0 with the same role on server 1723 and a non-empty new access token,
and the corrected second Login sent the selected UID. The game server then rejected
that role Login with `ec=28`, `ep=[E005]`; exit code 2 was reported. The guard again
sent no `dragon.assign.player.info` request and created no member output.

This reproduces the exact unresolved own-device sequence already documented from
2026-10-04 in `docs/auth.mdx` and `docs/live-validation.mdx`: fresh email binding,
successful loginKey exchange, then Android base-zone Login rejected with E005. The
role-UID correction is still valid because it removed the preceding blank guest wire
shape and changed E011 to the historically expected E005, but it does not solve the
broader fresh-device acceptance wall. E005 alone does not identify the cause. The
previous captured-token platform mismatch was separately solved; this run used the
Android identity consistently. ShuMei absence is also already ruled out by successful
captured-session reconnects without it.

Confirmed live: account verification, role UID, server 1723 and GSL token issuance.
Unverified: game-role authentication, Desert endpoint semantics and all member THP.
Exact next action is offline/static comparison of the Go-generated Android device ID
and Login payload with the version-matched real-client role-login path, beginning with
the known 36-character fallback device ID versus the previously observed 40-character
real-client ID. Do not request more email codes or vary live fingerprint fields by
trial and error. Preserve all failed private state directories and keep THP unknown.

## E005 diagnosis checkpoint — privacy-safe Login comparison (2026-10-12)

Focused static and protocol review found no evidence-backed Login change that can be
made from the existing material. The only successful packet capture is an older
iOS/Mac Catalyst session; comparing its device identifier, `ta`, latency or optional
fields directly with the fresh Android bind would cross platform and build paths.
`updateType=3` is not a forced-update result in the implemented version flow (`2` is);
the live E005 must not be attributed to that value. Empty ShuMei is proven acceptable
only for an established captured session, not for a fresh Android identity. E005 is
therefore still an authentication/fingerprint blocker rather than THP endpoint evidence.

The `pcap` utility now provides `-login-shape`, a credential-free offline JSON report
for a captured SFS Login. It records outer/Login/parameter field names and wire types,
safe package/platform/build values, lengths, validated device-ID shape and suffix,
UID/air-key/TA-zone relationships, TA key JSON types, response classification and only
strictly allowlisted `E[0-9]{3}` rejection labels. Missing comparisons remain absent,
and multiple Login requests or responses are marked ambiguous rather than paired.
It never prints stable identifiers, credentials, raw analytics values or packet bytes.
The raw capture remains private and must stay outside Git.

Automated verification on the VM's permanent Go 1.27.2 Linux ARM64 toolchain: focused
`cmd/pcap` tests, vet and build pass. A full sequential race suite, repository vet and
both CLI builds passed before the final review-only hardening; the focused checks then
passed again after that hardening. A GPT-6 Astra high-reasoning review found and then
confirmed fixes for ambiguous request/response pairing, absent-field equality, safe
E005/E011 distinction, suffixed device classification, outer wire types and null TA
zone handling. This is automated/static verification only; no new capture was taken,
no game request was sent, and no VM deployment or live database change occurred.

Exact next action: obtain one version-matched successful Android cold-start role Login
capture from the official app, keep it private, and run `pcap -login-shape` against it.
Compare that safe manifest with the failed Go Login manifest before changing any auth
field. If mobile capture is not operationally available, the blocker requires user
choice of a capture route; do not spend further email codes or guess fingerprint data.
Only after role Login succeeds should the guarded, single read-only THP request resume.

## Live milestone — official Login replay and 100-member THP (2026-10-13)

A Windows Google Play Games cold start was captured privately with built-in `pktmon`
while the user completed the official email-code login. The capture stayed outside Git.
The gap-tolerant offline decoder recovered one APS1723 Login request and its response;
the response was decoded as accepted and the user independently observed a successful
login. The credential-free manifest showed Android package/platform/storefront, a
40-character suffixed device ID with a derived 62-character air key, build
`1.0.361`/`151403`, resource version `104.88_124F_39581.23576`, non-empty ShuMei and
data-config values, and the full official Android telemetry/device field shape. No
identifier, token or raw capture is committed.

Two staged replays isolated the authentication requirement. Preserving only the build
and resource version still returned `ec=28/E005`. Preserving the complete encoded
parameter object from the server-accepted Login, while overriding only explicit
role/token/build fields, produced `login OK`, a complete bootstrap and observed source
server 1723. Captured templates deliberately refuse server redirects because opaque
UID/zone-bound fields cannot safely be rewritten; the operator must recapture instead.
`dataConfigMd5` and the encoded template are treated as private alongside the token.

The guarded reconnect then sent exactly one read-only
`dragon.assign.player.info {}` request. Live result: response status `ok`, exactly 100
unique records containing UID, display name and THP, zero rejected rows, source server
1723, and probe status `complete`. The normalised JSON is private on the user's laptop
outside the repositories and has not been ingested, printed or committed. Endpoint and
server coverage are live-verified; alliance scope still requires the user's manual
in-game comparison. `complete_roster` remains disabled and missing observations must
remain unknown.

Implementation changes in this milestone: `pcap` now resynchronises after capture gaps;
`-session-out` preserves accepted `resVersion` and the private encoded Login template;
`-member-thp-output` accepts either its original verified email/loginKey route or a
server-accepted reconnect session; bootstrap logging no longer prints unrelated private
push payloads; and the Windows capture helper checks every `pktmon` exit code. Automated
verification includes focused portable tests and Windows builds. On the VM's permanent
Go 1.27.2 Linux/ARM64 toolchain, `go test -race ./internal/app` passed, followed by
`go test -p 1 ./...`, `go vet ./...` and builds of both `cmd/lastwar-client` and
`cmd/pcap`. The full suite caught and resolved one stale test allowlist entry after
`dataConfigMd5` became sensitive; the complete rerun exited successfully.

Exact next action after this checkpoint is for the user to compare the private 100 rows
with the Canyon/Desert participant list. After manual scope confirmation, collect a
same-session `al.rank` roster and compare UID sets before enabling ingestion or any
roster-completeness claim.

## Manual scope confirmation and MVP direction (2026-10-13)

The user confirmed that all 100 returned names are the expected DoAC member set and
spot-checked 10 THP values against the in-game participant selector, including players
whose stable UIDs carry non-1723 suffixes; all 10 matched. Treat the guarded
`dragon.assign.player.info` result as live-verified DoAC THP coverage for display and
history. Continue to treat UID suffixes as historical/origin information rather than
current server scope, and keep `complete_roster=false` until `al.rank` semantics are
independently verified for membership transitions.

The dashboard branch now contains the reviewable repeatable-MVP bridge and runner: once
daily after the 02:00 UTC reset, execute this exact-100 guarded probe, import successful
normalised results transactionally, and record authentication/session failures without
inventing zero scores. No client code changed in this milestone. The accepted private
session has not been copied to the VM and no feature-branch binary, schedule, dashboard
or live database has been deployed. Exact client next action, after deployment approval,
was completed on 2026-10-13: the feature-branch collector was built on the VM using its
permanent Go 1.27.2 ARM64 toolchain and installed privately with the accepted session.
The dashboard imported the manually verified 100-member baseline and schedules isolated
processes at 04:00, 04:15, 04:30 and 04:45 UTC, skipping later retries after success.
No collector login was triggered during deployment verification. Exact client next
action is to inspect the first scheduled process on 2026-10-14 and confirm a successful
reconnect and exact-100 output; exit 2 must remain a visible
`authentication_required` failure and must not trigger unattended email verification.

## Live ranking expansion — base level and weekly VS (2026-10-13)

The read-only analytics collector now supports alliance-scoped `al.rank` base-level
observations using `mainCityLv`, while retaining UID as the stable key. Live validation
used the accepted server-1723 session. A fresh participant THP query returned the
manually verified 100-member set; the public type-13 THP ranking returned 200 rows.
Their UID intersection contained 11 records, all carrying the same non-empty alliance
identifier. That identifier is stored privately and is not logged or committed.

Using that verified scope, a live base-level request returned 100 records with zero
rejections, and a live weekly VS request returned 99 records with zero rejections.
These results verify endpoint parsing and current coverage, not authoritative roster
completeness. In particular, the missing VS observation remains unknown rather than
zero, and no membership departure is inferred. The weekly VS capture has the explicit
provisional period `live-probe:2026-10-13`; its game-week semantics are unverified and
it must not be combined with summed daily VS observations.

Automated verification on the VM's permanent Go 1.27.2 toolchain passed focused
analytics tests, the application tests with the fixture-compatible umask, vet and a
client build. The reviewed binary was installed privately. The dashboard now imports
the verified base-level and weekly-VS captures under the same private numeric scope.
Exact next protocol action is to establish the active VS game-week identifier and
day-1-to-day-6 mapping from live/static evidence, then add idempotent daily VS
collection without changing the weekly series. The first unattended THP run remains
due at 04:00 UTC on 2026-10-14 and should be checked independently.

## Dashboard contract milestone — UID-joined VS views (2026-10-13)

The sibling dashboard now joins all player-facing VS projections on the collector's
stable UID rather than display name. It is ready for current-week day columns,
weekly-ranking total/6 history, multi-week day-of-week analysis, and conservative
membership counts. Missing UID/day rows remain unknown and explicit zeroes remain zero.
An additive game-day mapping prevents historical VS day denominators from using a
roster observed on the later query date.

No client protocol behaviour changed in this milestone. Exact client next action is
still to verify the game-week identifier/day-1-to-day-6 mapping and daily ranking
coverage, then collect daily membership observations by UID. `complete_roster` must
remain false until live completeness is independently proven; only successive complete
rosters may establish joins or departures.

## THP performance analytics contract (2026-10-13)

The sibling dashboard now consumes the existing guarded daily member THP series for
UID-keyed growth, relative growth, robust trend, momentum and base-upgrade context. It
also has an aggregate-only runner for the already live-verified server-1723 type-13
top-200 query. No client protocol code changed. The raw public ranking is used privately
to calculate count/sum/median and adjacent-capture matched growth, while SQLite retains
no top-200 player identities. The first live benchmark returned exactly 200 rows with
matching server metadata and no per-row rank field; the dashboard accepts that
consistent live shape while rejecting mixed/duplicate rank shapes.

## Event discovery and live availability (2026-10-13)

Static Lua and a guarded live server-1723 probe now establish the meteorite discovery
path. `meteorite.act.info` is read-only and returns `beginTime`, `stage`,
`stageEndTime` and zero-based `grabTimes`; Lua maps stages 1–5 to Match, Preview,
Grab, Rest and Results. The live response was Preview, round 0, beginning
2026-10-13 02:00 UTC and ending that stage 2026-10-17 02:10 UTC. Raw responses remain
private. `meteorite_auto` records only those allowlisted fields and expands into
round 0..`grabTimes` personal ranking queries only from Grab onwards, so schedules and
rounds no longer depend on an operator prompt or guess.

A separate authenticated live probe on the observed source server 1723 returned
explicit empty payloads for candidate SvS personal score and season personal power
types 2 and 4. This proves the commands remain callable now, not that last weekend's
SvS never existed; the SvS endpoint has no history selector. Nothing was interpreted
as zero or imported as member performance. Season devotes remains unqueried because
no active event ID/subtype has been established.

Automated verification: full `go test ./...` passed in a disposable VM checkout with
Go 1.27.2. Live verification: the schedule fields above and empty ranking shapes.
Unverified: meteorite ranking coverage once the event reaches Grab, event retention,
alliance coverage, and season contribution semantics. Immediate next action is to run
the automatic meteorite collector during/after the first Grab stage, measure UID
overlap with the verified 100-member cohort, and retain missing members as unknown.

Deployment checkpoint: the tested binary was installed privately with the previous
binary retained as `lastwar-client.pre-events`. The first unattended run reproduced
the Preview context and correctly issued no meteorite ranking request. The sibling
event timer will retry daily and hourly at weekends; exact next action is automated
round/UID coverage review after the 2026-10-17 02:10 UTC Preview boundary.

## Next-season opponent intelligence boundary (2026-10-13)

The sibling dashboard specification now covers four selected opponent warzones and
alliance drilldowns. Existing protocol evidence supports target-server type-13 THP and
statically identifies type 4 player power, while `al.rank` can address a known alliance
ID. None of that proves opponent access, rank caps, warzone membership or completeness.
There is no implemented/verified query for warzone-to-server mapping, top alliances by
power or total warzone power. Do not infer these from command names.

At next-season discovery, first trace the warzone mapping and alliance-power UI/handler,
then live-probe type 13 and type 4 for one explicitly selected server. Preserve alliance
ID/name fields currently dropped by the generic exporter before building scouting
summaries. A claimed top-200 THP total requires exactly 200 distinct, consistently ranked
UID rows; opponent alliance overlap requires private current-season UID retention.

## Random-server scouting milestone (2026-10-13)

The accepted server-1723 session can read server 1724 without changing game state.
Live type-13 THP and type-4 ordinary-player-power probes each returned 200 valid unique
UID rows with matching server metadata and zero rejects. The cohorts overlapped by 169
UIDs; their aggregate values were respectively 25,043,972,145 THP and 41,930,428,528
power. Per-row ranks were absent in both responses, so the correct current label is an
observed 200-row server cohort rather than a proven complete top-200 ranking.

All THP rows had alliance IDs across 11 alliances; power rows covered 10 alliances. An
`al.rank` request for the alliance most represented in the power cohort returned 95
valid UID-keyed members, all on server 1724 and in the requested alliance. This proves
opponent roster readability for one sample, but not full-roster semantics; keep
`complete_roster=false`. Identities, alliance IDs and captures remain private.

Client code now recognises analytics query kind `player_power`, issuing `rank.get` type
4 and strictly parsing `power`. Focused analytics tests passed in the disposable VM
checkout before the live request. Type 4 is live-verified for server 1724, while warzone
mapping, alliance-power ordering and roster completeness remain unverified.

Season-battle activity is a separate discovery track. The best static candidate is
`lw.camp.battle.user.score.rank {}`, but its row schema, event/round identity, score
semantics, retention and opponent scope are unresolved. Exact next action is to trace
its version-matched manager/UI consumer and schedule, then make one private read-only
applicable probe. Do not interpret missing rows as non-participation. In parallel, the
next scouting action is discovery of the server/warzone mapping and top-alliance power
endpoint; represented alliances in a capped player cohort are not necessarily the true
top ten alliances.

Follow-up static tracing identified the exact season-2 faction-war score-list UI. It
sends `lw.camp.battle.user.score.rank {}` on open and renders `list[]` rows by rank,
score, UID/name, server and alliance abbreviation; “my alliance” is only a client-side
filter. The client now supports this as provisional metric `season_battle_score`. Focused
analytics/application tests passed on Go 1.27.2. A live server-1723 probe was accepted
and returned an explicit empty list with no error and zero rejects. This verifies
callability and empty-payload handling only; retention and Wednesday/Saturday battle
windows remain unverified. Exact next action is to collect before, during and after the
next applicable Wednesday/Saturday faction battle and compare score changes, without
deploying the temporary binary or importing the empty probe.

## Codex persistent-permission diagnosis (2026-10-14)

Codex CLI 0.160.0 is active. “Always approve” has accumulated exact
`prefix_rule` entries in `C:\Users\edwar\.codex\rules\default.rules`; it did not save a
semantic permission for SCP, Git or GitHub. On Windows the matcher sees the runtime
PowerShell executable, `-Command`, and the entire command text as three arguments.
Changing a source file, destination, quoting, option order, compound command or
PowerShell executable therefore misses the saved rule and prompts again. The active
managed profile remains workspace-write/network-restricted with on-request escalation;
no deny/prompt rule was found overriding a matching allow rule.

One fixed no-argument entry point, `tools/codex_doac_operation.ps1`, dispatches only to
two reviewed no-argument helpers: `codex_vm_transfer.ps1`
accepts only tracked DoAC files or bounded staged artefacts, rejects sensitive paths and
uploads one bounded archive to `/tmp/doac-dev/incoming/`; `codex_publish.ps1` can only
push the two named feature branches without force or update their existing draft PR #1
from fixed ignored body files. Both validate ignored manifests and fixed repository,
remote and branch identities. A single exact invocation-only rule was checked with the staged
CLI 0.162.1 `codex execpolicy check`: intended calls allow; helper arguments, direct SCP
to production, arbitrary PowerShell and force-push-main examples do not match. The active
0.160.0 CLI has no `execpolicy` subcommand, which is the installed-version limitation.
The user approved that exact rule and it is now installed; no sandbox setting was
weakened. The remaining acceptance check is one bounded real transfer to the isolated
incoming directory plus a policy-only rejection check for a direct production-path copy.
