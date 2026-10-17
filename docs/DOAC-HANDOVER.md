# DoAC Last War analytics — technical handover

Last updated: 2026-10-11. This document preserves findings from the user's ChatGPT research and development session. **Observations are not necessarily verified against the latest server or game build.** Never commit credentials, tokens, game session keys, account identifiers or private dashboard data.

## Purpose and scope

Build historical analytics for the **DoAC** alliance on **Last War: Survival server 1723**. The desired dashboard is UID-keyed, using SQLite for daily historical snapshots and Flask for display. Metrics: Total Hero Power (**THP**, keep that abbreviation), individual daily and weekly Alliance Duel/VS scores, alliance membership changes, season contribution/ranks, SvS, meteorite scores and trends. Prefer read-only game queries. No gameplay-changing requests (especially `-collect`) as part of analytics.

## Infrastructure and working directories (historical, verify before use)

- Oracle Cloud Always Free ARM VM, Oracle Linux 9.8; SSH user `opc`; user typically operates from Termius on an iPhone.
- Flask/Gunicorn + Podman dashboard: `/home/opc/doac-dashboard`, served locally on port 8080, exposed via a Cloudflare Quick Tunnel. Do not assume a stable public URL.
- Direct-protocol Go client originally cloned from `https://github.com/ljagiello/lastwar-client` to `/home/opc/lastwar-client`.
- Personal fork: `https://github.com/mysteryhawk/lastwar-client`. Active development branch: `feature/interactive-continuous-reader`, draft PR **#1** (`https://github.com/mysteryhawk/lastwar-client/pull/1`). `main` must not be changed/merged without approval.
- Lua reverse-engineering files on VM under `/home/opc/lastwar-lua` (not in GitHub); do not assume Work has VM access.

## Data-source findings

A third-party LastWar Tools alliance-roster endpoint was accessible for the alliance, but VS endpoints require a session key and profession information appeared inaccurate. **Do not treat this third-party API as a reliable authenticated source** or expose session keys to it. The owner prefers direct protocol and has declined to provide game credentials to third parties.

The Go client uses SFS2X (SmartFoxServer) direct protocol. The guest-login path `-no-config -list-buildings` can create a guest on first login, but the created guest was assigned to **server 1456**, not target **1723**. Attempts to log back into the same guest subsequently failed with `LOGIN FAILED: ec=28 E011`, even with a fresh GSL token and isolated identities via `$LASTWAR_STATE_DIR`. This may be a returning-guest auth bug; do not assert its root cause without testing. Starting `-interactive` on the **first** login of a new guest works, permitting multiple commands on the same connection. Guest account is not a DoAC member, so an alliance request may return no alliance data; a valid DoAC account/session or legitimate target-server access may eventually be necessary. Avoid asking the user to paste game credentials into chat.

Historical live interactive session (likely stale by the time this is read):
- Process PID `109777`, command `./lastwar-client -no-config -interactive /tmp/lastwar-control -log-level info`.
- Control FIFO: `/tmp/lastwar-control`.
- `printf '%s\n' 'al.rank' > /tmp/lastwar-control` returned a success-shaped response containing `_id` and `_time`, but no roster payload (guest is not in alliance).
- The user has explicitly said preserving this session is **not** essential; a new guest can be created. Do not spend disproportionate effort keeping this old process alive.

## Diagnosed interactive read/backlog problem

On the old VM session, `ss -tnp` showed a game TCP connection from `10.0.0.230:48912` to `136.107.113.253:10532`, PID 109777. The socket's **Recv-Q** was initially **235,438 bytes**, then **250,421**, **250,467**, **250,490**, **250,513**, **250,536** bytes across five ~5-second checks. This is strong evidence that the client was not continuously draining unsolicited incoming game messages while idle.

Original interactive command path in `internal/app/interactive.go`:
1. Read FIFO command.
2. `conn.SendExtension(cmd, params)`.
3. `session.WaitForCmd(conn, session.DefaultCmdTimeout, cmd, "push."+cmd)`.

`session.WaitFor` in `internal/session/wire.go` reads incoming envelopes until a matching command appears, with `MaxNonMatchingEnvelopesPerWait = 1000`. Large accumulated push traffic can exhaust that limit. Observed requests:
- `al.battle.rank.info {"type":0,"day":1}` at 2026-10-10 23:06 UTC: logged send, then `no matching response within 8s`, with `1001 well-formed but non-matching envelopes processed`.
- `al.battle.rank.info {"type":1}` at 23:14 UTC: same error almost immediately (7 ms).

The 1001 nonmatching messages do **not** by themselves prove the server refused the requests. A proper continuous reader, unsolicited push handling and **request `_id` correlation** are needed.

## GitHub work already started

Draft PR #1 on `feature/interactive-continuous-reader` contains:
- `internal/app/interactive.go`: starts `startInteractiveReader(conn)` when interactive mode begins, changes interactive waits to consume a channel rather than reading the socket concurrently.
- `internal/app/interactive_reader.go`: a goroutine continuously reads `conn.ReadEnvelope()` and queues extension messages in a bounded channel; command loop matches by command name.

**Important: This first patch was not compiled, tested or deployed. It is an exploratory draft, not production-ready.** It lacks `_id` correlation, uses a bounded queue that can drop messages and might lose matching responses, and may need better shutdown, error propagation, diagnostics and response registration before send. Audit tests and compile immediately. Do not assume this design is correct merely because it was committed. `main` remains untouched.

## SFS wire details and typed parameters

- `internal/session/conn.go`: `GameConn` owns `net.Conn`, `bufio.Reader`, a write mutex and extension-request counter. `SendExtension` assigns an Int `_id` (starting after login's ID 1) and wraps command `c`, room `r=-1`, and params `p` into the extension envelope. The server typically echoes `_id` and adds `_time`.
- `ReadEnvelope` reads SFS packets; `Envelope.AsExtension()` extracts the extension command and parameters. `ReadEnvelope` also handles terminal pushes.
- Existing interactive JSON parser calls `putJSONValue`; **every JSON integer is currently encoded with `PutLong`**, even where Lua calls `PutInt`. Preserve large 64-bit IDs but allow explicit per-field SFS types.
- A robust dispatcher should use **one and only one socket reader**, handle unsolicited pushes without accumulating an unbounded backlog, match responses to pending requests by `_id` where available, and tolerate command-name variations. Consider whether response registration must precede send and how to handle messages without `_id`. Preserve existing non-interactive behaviour unless intentionally migrated.
- Avoid logging sensitive request/response values: existing `StringRedacted()` is intended for safe diagnostics.

## Official Lua client analysis (historical build)

Source archive used:
`https://lastwar-cdn.akamaized.net/hotupdate/com.fun.lastwar.gp/Android/LWScripts_826_u440.bz2`

Extracted archive: `~/lastwar-lua/LWScripts_826`; custom `LWLF` header, version 826, about 18,740 Lua bytecode modules. Extraction script: `~/lastwar-lua/extract_lua.py`; outputs `~/lastwar-lua/extracted/NNNNN.luac`, `matches.tsv`, `modules.tsv`. For Lua 5.3 bytecode, the observed module header required restoring format byte and instruction-size byte for decompilation:

```python
fixed = data[:5] + b"\x00" + data[6:14] + b"\x04" + data[14:]
```

Fixed modules: `~/lastwar-lua/fixed/NNNNN.luac`. Java 17 and `HansWessels/unluac` were used; JAR at `~/lastwar-lua/unluac/bin/unluac.jar`. Example:

```bash
java -jar ~/lastwar-lua/unluac/bin/unluac.jar ~/lastwar-lua/fixed/NNNNN.luac > /tmp/module.lua
```

Decompiled `00587.luac` → `Net/Config/MsgDefines.luac` (`decompiled/MsgDefines.lua`) and `00588.luac` → `Net/Config/MsgMap.luac` (`decompiled/MsgMap.lua`).

### Relevant protocol command strings

| Meaning | Command |
|---|---|
| Alliance Duel/VS rankings | `al.battle.rank.info` |
| My alliance duel league info | `get.alliance.duel.season.info` |
| Current season league group | `get.alliance.duel.group.info` |
| Alliance season score ranking | `get.alliance.season.score.rank` |
| Player/server ranking | `server.rank` |
| Ranking list | `rank.get` |
| Ranking preview | `rank.get.preview` |
| Alliance rank | `al.rank` |
| Season contribution | `season.contribution.info` |
| Season ranking | `lw.season.rank.info` |
| Alliance devotes ranking | `lw.season.alliance.devotes.rank.info` |
| Alliance member devotes | `lw.alliance.devotes.rank.all` |
| Season faction battle user scores | `lw.camp.battle.user.score.rank` |
| Season trend ranking | `lw.season.trend.rank.info` |

### Lua handler findings

`AllianceCompeteRankList.lua` sends:
- `PutLong("type", type)`.
- For daily type, `PutInt("day", realDay)`, where day is clamped to **1..6** and defaults to current weekday.
- The handler passes the response to `DataCenter.AllianceCompeteDataManager:RefreshRankList(t)`.

`AllianceCompeteDataManager.lua`: `FetchRankList(type, day)` sends `AllianceCompeteRankList`. `RefreshRankList` parses `message.rankInfo`. In `UIAllianceCompeteRankView.lua`, tab types are **0 daily, 1 weekly, 2 season**. Ranking records include `uid`, `aid`, `name`, `score`; day/week UI filters by the current alliance's `aid`. **Check real server response structure before finalising a parser.**

`GetRankList.lua` sends `PutInt("type", theType)`, `PutInt("global", global)`, `PutInt("serverId", serverId)`.

`GetSeasonRankInfo.lua` sends `PutInt("rank_type", rank_type)` and `PutInt("pageSize", pageSize or 100)`.

`PlayerRankList.lua` sends `PutInt("ismerge", 0)`.

`RankDataManager.lua` parses `message.serverRanking` into `PlayerRankData`. `PlayerRankData.lua` fields include `uid`, `name`, `abbr`, `alliancename`, `power`, `heroPower`, `rank`, `serverId`/`srcServer`, `score`. For **THP**, investigate `heroPower` rather than assuming `power` is equivalent. Find the numeric `RankingTypeServer.HERO_TOTAL_POWER` constant from the Lua definitions; do not guess it.

## Design and next work

1. **Build/test the current draft PR** (Go formatting, `go test ./...`, race tests where practical). Inspect whether the initial reader/channel change introduces regressions. Improve rather than merging untested code.
2. Implement a persistent read loop with correct lifecycle, error propagation, bounded handling of unsolicited pushes and request/response correlation by `_id`. Preserve compatibility with other callers of `ReadEnvelope`.
3. Add tests with a fake SFS game server: idle push bursts >1000; replies interleaved with pushes; mismatched/stale replies; terminal pushes; disconnections; deadlines; no simultaneous socket readers; FIFO command behaviour.
4. Add a typed, **read-only** request interface; encode `type` as SFS Long for `al.battle.rank.info`, `day` as Int, and ranking parameters with correct Int tags.
5. Implement/verify VS daily, weekly and season ranking retrieval. Inspect raw **redacted** responses, alliance ID filtering, UID identity and date/day mapping. Be aware that a random guest on server 1456 cannot necessarily access DoAC/server 1723 data.
6. Locate `RankingTypeServer.HERO_TOTAL_POWER`; implement THP server ranking parsing and, if necessary, pagination.
7. Build a minimal structured export (JSON/JSONL) with timestamp, source server, UID, alliance ID, metric, period and validation metadata. Then integrate with UID-keyed SQLite history and Flask dashboard.
8. Maintain documentation, changelog and draft PR; keep credentials out of logs and commits. Do not merge or deploy without user approval.

## User preferences and guardrails

- UK English, concise instructions; user is primarily on a phone and wants to avoid repeated manual Termius copy/paste.
- Work on **personal** GitHub `mysteryhawk`, not the employer's repositories.
- GitHub connector was initially read-only (403 on branch creation) until GitHub App installation was completed; subsequent branch creation and commits succeeded.
- GitHub access is not VM access. Do not claim code has been tested on the VM or deployed unless that has actually happened.
- Never request or expose personal access tokens, SSH private keys or game credentials in chat. Prefer official scoped OAuth/connector access.
- Keep collection read-only. Obtain explicit approval before any gameplay-changing actions, merges to `main`, or deployment.

## Handover to ChatGPT Work

Read this file and PR #1 first. Review the full source and tests before continuing. The historical Lua extraction files are **only on the Oracle VM**, not in the GitHub repository. Clearly distinguish confirmed observations from hypotheses, and record new protocol discoveries in version-controlled docs. The user's immediate priority is a robust direct-protocol collector that can ultimately populate DoAC's historical dashboard.
