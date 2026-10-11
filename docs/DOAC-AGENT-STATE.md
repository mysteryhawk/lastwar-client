# DoAC agent state

Updated: 2026-10-11. Read this alongside [DOAC-HANDOVER.md](DOAC-HANDOVER.md).

## Current state

- Active branch: `feature/interactive-continuous-reader`; draft PR #1. Main remains unchanged.
- Recovery complete: read original handover, PR diff, repository session/app/SFS source and existing tests. Earlier conversation retrieval confirms the handover; no additional THP constant or dashboard schema recovered.
- Developing: replace command-name queue with a continuous, `_id`-correlated dispatcher, then typed read-only analytics and historical exports.
- Blocked: live game validation and VM-only Lua constants/schema require environment access not currently available. No credentials requested or used.
- Immediate next action: obtain the repository-required Go toolchain, establish baseline tests, implement dispatcher.

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
