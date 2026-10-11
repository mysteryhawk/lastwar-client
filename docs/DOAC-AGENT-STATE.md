# DoAC agent state

Updated: 2026-10-11. Read this alongside [DOAC-HANDOVER.md](DOAC-HANDOVER.md).

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
