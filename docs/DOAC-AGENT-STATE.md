# DoAC agent state

Updated: 2026-10-11. Read this alongside [DOAC-HANDOVER.md](DOAC-HANDOVER.md).

## Current state

- Branch `feature/interactive-continuous-reader`; draft PR #1. Main unchanged, no VM deployment, no game-server calls in this session.
- Committed transport milestone: `20667c2` (dispatcher, typed scalars, updated tests/state). Initial state: `bbfcd76`.
- Implemented next milestone: read-only VS daily/weekly/season, THP type 13, roster, season personal power/devotes, candidate SvS and meteorite queries; strict parsers; private JSONL CLI; transactional SQLite importer.
- Automated verification: original full suite and transport/app/SFS race suite passed; latest collector/CLI race tests and five Python importer tests passed. Whole-repository `go test ./... -race`, `go vet ./...`, native build, Linux ARM64 build and gofmt checks passed. Python tests also added to CI.
- Live verification: none here. Historical upstream roster evidence remains clearly labelled. Full-roster marking deliberately disabled until live completeness/access semantics are checked.
- Blocked: target-server/alliance live session, actual dashboard schema and active event IDs/rounds. THP constant is no longer blocked: recovered from the public historical Lua bundle.
- Immediate next action: commit this milestone and update PR; then live validation/integration requires authorised environment access.

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

## Priorities and next actions

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
1. Finish full race/build/vet/ARM checks; commit code/tests/docs together and rewrite PR around read-only analytics.
2. In an authorised existing VM/session environment, collect redacted payload schemas for VS/THP/roster; measure idle Recv-Q and compare roster UIDs against THP coverage. Do not create a guest automatically or mislabel server 1456 as 1723.
3. Confirm roster is complete before enabling complete_roster; then verify membership events from successive real snapshots.
4. Identify active season event ID/subtype, meteorite round and SvS event applicability; validate builders against live replies. Unknown structures remain missing implementation/evidence, not claimed unavailable.
5. Read the existing Flask/SQLite schema and adapt its queries to the new history DB. No live DB migration or deployment authorised.
6. Confirm leaderboard truncation and determine a documented read-only per-member THP source if ranking coverage is incomplete. No paging params occur in rank.get's historical handler.

Final safety review refinement: writer errors now terminate the dispatcher even after the caller cancelled; top-level params are copied before asynchronous send. No live data/captures/authentication material created or committed. Source records remain private, with ignored JSONL/SQLite outputs.

Final ownership review: `ReadEnvelope` now rejects callers after dispatcher ownership starts (`ErrReaderOwned`); bootstrap reads and ownership transfer are serialised. This prevents accidental competing readers instead of relying only on documentation. Normal-login analytics branch removed as unreachable because analytics requires reconnect configuration.
