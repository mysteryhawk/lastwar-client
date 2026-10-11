# DoAC agent state

Updated: 2026-10-11. Read this alongside [DOAC-HANDOVER.md](DOAC-HANDOVER.md).

## Current state

- Active branch: `feature/interactive-continuous-reader`; draft PR #1. Main remains unchanged.
- Recovery complete: read original handover, PR diff, repository session/app/SFS source and existing tests. Earlier conversation retrieval confirms the handover; no additional THP constant or dashboard schema recovered.
- Automated verification: continuous dispatcher and typed scalars implemented; full `go test ./...` passed after correcting draft signature and fake-server ID echoes.
- Developing: typed read-only analytics, historical exports and targeted Lua investigation.
- Blocked: live game validation and VM-only Lua constants/schema require environment access not currently available. No credentials requested or used.
- Immediate next action: implement analytics collectors and recover THP constant from the publicly hosted historical Lua bundle.

## Architecture and environment

Go module `lastwar-client`, Go 1.27.2, klauspost/compress 1.20.1. `internal/session/conn.go` owns packet reads, serialised writes, extension ID sequence, heartbeat and terminal-session handling. `wire.go` provides synchronous command waits. Login and existing game features depend on these synchronous reads; preserve them before entering analytics/interactive mode. `internal/app/interactive.go` owns FIFO parsing and process shutdown. `internal/sfs` implements typed SFS2X objects/framing. Existing game features include gameplay-changing requests; analytics must never invoke them.

Oracle Linux ARM VM paths from handover: `/home/opc/lastwar-client`, `/home/opc/lastwar-lua`, Flask/SQLite `/home/opc/doac-dashboard`. Neither VM deployment nor its dashboard schema has been inspected in this session.

## Confirmed findings and decisions

- PR head at recovery: `7a4ac230094a86b23b60decd4bf0a475b016fb9f`; base main `312071b9b561fbc06e80b411b7d3e1bd228fd63c`.
- Draft changes `handleInteractiveLine` signature without updating existing callers in tests. Initial reader queues all extension messages (256 slots), drops overflow and matches by command only. It is not verified and will be replaced.
- Register pending requests before sending. Require echoed Int `_id` and expected command (including `push.` alias); never fall back to command-only matching. Missing IDs are pushes, not evidence of command failure.
- Socket deadlines must be cleared before continuous reading. Request timeouts must not interrupt socket framing: retrying packet reads after a partial-frame timeout can desynchronise the stream.
- Repository `docs/live-validation.mdx` documents upstream live evidence (2026-10-06): `al.rank {allianceId: Utf}` returns `list[]` roster. This is stronger evidence than the old empty no-parameter guest request. It is not live verification by this session.
- VS types/field tags and rankInfo fields are historical Lua findings in the handover. THP is `heroPower`, distinct from `power`; numeric HERO_TOTAL_POWER remains unknown. Do not guess it.

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
