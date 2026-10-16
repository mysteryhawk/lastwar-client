# DoAC protocol findings — 2026-10-11

Most findings below are **static analysis of the official historical client**. The dated
live-verification section is the explicit exception. The collectors also have automated
synthetic tests. Keep all three evidence levels separate.

## Live verification — 2026-10-11

Fresh first-login guests on several observed source servers authenticated successfully;
returning guests continued to fail with E011 before extension requests. From fresh guests
on source servers other than 1723, `rank.get` type 13/global 0/serverId 1723 returned
100 rows with `heroPower`, stable UID, `srcServer`, `allianceId`, `abbr` and
`alliancename`. Eleven rows were tagged DoAC in repeated probes. This confirms live
cross-server visibility for that exact THP query shape, not for arbitrary rankings.

The single consistent DoAC `allianceId` was derived privately from those rows and used in
`al.rank`. The response contained a `list` with 100 unique UIDs, all explicitly on server
1723. Two consecutive calls in one authenticated session returned the same UID set. All
11 DoAC UIDs visible in the top-100 THP result were present in the roster; 89 observed
roster UIDs had no THP row. Neither result is proven complete: 100 may be a server-side
limit, and no independent member-count field was present. Keep `complete_roster=false`.

Daily VS type 0/day 1, weekly type 1 and season type 2 returned correlated,
success-shaped responses with `rankInfo` present but no UID/score rows. This establishes
transport success and an empty response in those guest/event contexts only. It does not
show that VS is unsupported or unavailable to a participating alliance account.

## Reproducible source

Source: `https://lastwar-cdn.akamaized.net/hotupdate/com.fun.lastwar.gp/Android/LWScripts_826_u440.bz2`.

- Archive SHA-256: `3ffe005adf44716bd9af020b71ea160e00d9a97bc1cb84cc0fa68cd2fb13c069`.
- Uncompressed LWLF: 106,819,620 bytes, version 826, 18,740 modules.
- Container: magic, three little-endian u32s (file version, Lua version, count); each entry is a .NET 7-bit-length string, little-endian u32 byte length, then Lua bytecode.
- `tools/extract_lua.py` extracts selected modules, fixes format byte 1 to 0 and restores the omitted instruction-size byte, and writes a hash manifest. No account or VM access required.
- Decompiler: `https://github.com/HansWessels/unluac`, revision `1b2bfed6045eb719a27f026311744d7a674847f5`. Standard fixed-header decompilation succeeded. This session compiled its Java sources using Eclipse ECJ 3.40.0 on Java 17 because only the Java runtime was installed. Ordinary JDK `build.sh` is sufficient elsewhere.

```bash
curl -fL -o /tmp/doac-lua.bz2 https://lastwar-cdn.akamaized.net/hotupdate/com.fun.lastwar.gp/Android/LWScripts_826_u440.bz2
python3 tools/extract_lua.py /tmp/doac-lua.bz2 /tmp/doac-lua-fixed \
  --match EnumType --match MsgDefines --match MsgMap --match Rank \
  --match AllianceMemberInfo --match AllianceCompeteDataManager --match ZoneWarManager
java -jar /path/to/unluac.jar /tmp/doac-lua-fixed/Global/EnumType.luac > /tmp/EnumType.lua
```

Do not commit the bundle, decompiled client sources or production captures. Relevant conclusions are recorded below instead.

## THP and server rankings

`Global/EnumType.luac`, `RankingTypeServer`: **HERO_TOTAL_POWER = 13**. POWER = 4; KILL = 3. THP is not city power.

`Net/Msgs/GetRankListMessage.luac` uses `rank.get`, with Int `type`, `global`, `serverId`. It routes type 13 to `RankDataManager:ParsePlayerRankData`. `DataCenter/RankData/RankDataManager.luac` reads `serverRanking`; `PlayerRankData.luac` independently parses `heroPower`, `power`, `uid`, `name`, `serverId`/`srcServer`, `rank`, `score`. The THP collector requires `heroPower`, with no fallback to `power` or zero.

No paging fields occur in this handler. The available ranking may be truncated; it does not guarantee THP for every alliance member. Compare roster UIDs against returned ranking UIDs during live validation. Do not invent missing THP values. `server.rank {ismerge:Int(0)}` routes to commander/city power, so it is not the THP query.

## VS

`Net/Msgs/Activity/AllianceCompeteRankListMessage.luac`: Long `type`, Int `day` for daily. `Global/EnumType.luac` has daily 0, weekly 1, season 2. `DataCenter/AllianceCompete/AllianceCompeteDataManager.luac`, `RefreshRankList`, reads `rankInfo`, response type and day. It walks the collection as a Lua table; parser supports SFS arrays and object maps. Known row fields: uid, aid, name, score. Daily/weekly records are filtered by explicit alliance ID. Season is retained as a ranking, not a roster.

Day is 1..6. A host-local weekday is not sufficient to identify the game day. Config requires explicit game period labels. Contradictory echoed type/day is rejected.

Version-matched VM Lua adds a useful context boundary. `UIAllianceCompeteRankView.CheckIfTabVisible`
hides the daily tab when the alliance-duel activity is absent, finished or lacks event info;
it hides the season tab unless `LeagueMatchManager:CheckIfInMatch()` succeeds, while the
weekly tab remains visible. `ChangeTab` sends the ranking request directly once a visible
tab is chosen, and the message/manager contain no additional participation guard. Therefore
a fresh guest's success-shaped empty result is consistent with missing event/account context,
not a protocol failure. The ranking request carries no week identifier; an observed duel
`weekEndTime` is the preferred stable input for the caller-supplied period label.

### Desert Storm participant THP source

Version-matched Lua confirms the Desert Storm selector sends
`dragon.assign.player.info {}` and consumes an unpaged `users` collection. The manager
stores every returned row by stable `uid`. `ActDragonPlayerData` parses separate
`heroPower`, `power` and `armyPower` fields, while `UIBFDesertSelectUserCtrl` explicitly
selects the hero-power display and the shared row renderer reads `heroPower`. This is a
strong candidate for all-member THP without the type-13 leaderboard cap. The request is
implicitly scoped to the logged-in alliance: it carries no alliance/server/page parameter.
Live DoAC coverage and event/access availability remain unverified.

The analogous Canyon/DSB command is `dsb.act.player.list {}` with unpaged `users`, but its
version-matched row parser exposes `power` and `armyPower`, not `heroPower`; do not use it
as THP evidence.

## Membership

Upstream historical live evidence: `docs/live-validation.mdx`, 2026-10-06, documents `al.rank {allianceId:Utf}` and `list[]`. No new live test occurred here.

`DataCenter/AllianceData/AllianceMemberInfo.luac`, `ParseData`: `userId` overrides `uid`; `playerName` overrides `name`. The collector implements those aliases. It preserves UID, name, alliance, player server and optional rank. It does not infer departures from empty or partial replies.

**Authoritative/full-roster marking remains disabled** in the Go collector until a live sample establishes completeness, pagination and access semantics. SQLite membership transition logic is tested with explicitly authoritative synthetic snapshots; live roster observations can already be stored and compared manually.

## Additional read-only rankings

| Query | Request fields (SFS type) | Response evidence |
|---|---|---|
| `lw.season.rank.info` | `rank_type:Int`, `pageSize:Int` | `SeasonDataManager:UpdateSeasonRankDataCache` uses `rank`/`ranks`/`ls`; `LWSeasonRankItem` displays `score`. `SeasonRankType.PersonalPower=2`, `PersonalCrossServerPower=4`. Other types describe alliances/servers/camps, not player UIDs. |
| `lw.season.alliance.devotes.rank.info` | `event_id:Utf`, `day:Int`, `start:Int`, `end:Int` | `LWSeasonAlliancedevotesRankInfoMessage`, `SeasonAllianceRankDataManager:UpdataRankData`: `ranks[]`, parsed as PlayerRankData. UI requests start=1,end=-1; event ID and subtype must be established from the active season. Collector requires row `score`. |
| `cross.throne.person.score.rank` | none | `CrossKingPersonScoreRankMessage` passes to `ZoneWarManager:SetPersonScoreRank`, which reads `ranks`; handler synthetic sample uses uid/name/score/rank. Candidate SvS personal-score query; exact event coverage requires live validation. |
| `meteorite.rank.person.info` | `grabTimes:Int`, `startRank:Int`, `count:Int` | `MeteoriteRankPersonInfoMessage` defaults start=1,count=100; `ActMeteoriteBattleManager` caches by grabTimes; `LWActMeteoriteRank:UpdateData` reads `data`; row UI uses uid/name/score/serverId. Round index is zero-based in the UI. |

These query builders/parsers are implemented and tested with synthetic responses. Live payload shapes, active-event access, score meaning and limits remain unverified.

## Unresolved rather than unavailable

- `season.contribution.info` appears in MsgDefines and maps to `Net.Msgs.Glory.SeasonContributionInfoMessage`, but no module matching Contribution exists in the downloaded 826 archive. This may be stale dispatch metadata or a separately delivered handler. Do not guess parameters or call reward operations to discover it.
- `lw.alliance.devotes.rank.all` has a no-parameter handler, but its manager stores the whole response without a clear individual-score shape. It is not parsed speculatively.
- `lw.camp.battle.user.score.rank` has a no-parameter handler which broadcasts the reply. It needs an identified UI consumer or safe live sample before adding a parser.
- Guest assignment to 1456 and historical returning-guest E011 are authentication/access observations, not proof any ranking command is unsupported. The current analytics CLI requires existing reconnect configuration and cannot auto-create guests.
- No VM access was required to recover the THP constant. VM access is still needed to inspect the existing dashboard schema and use its already-authorised session environment for live validation; no deployment is implied.

## Arbitrary-server scouting live evidence (2026-10-13)

Read-only probes from the accepted server-1723 role session targeted server 1724. The
type-13 THP request returned exactly 200 valid UID rows, all labelled server 1724, with
zero parser rejects. Every row carried an alliance ID and the cohort represented 11
alliances. The live response consistently omitted per-row `rank`; it is therefore an
ordered server cohort of observed size 200, not yet proof of a formally ranked or
page-complete top 200. Its aggregate THP was 25,043,972,145.

The statically identified type-4 player-power request was then implemented as analytics
kind `player_power` and live-verified against the same target. It returned exactly 200
valid server-1724 rows with zero rejects, represented 10 alliances and had aggregate
power 41,930,428,528. The THP and power cohorts shared 169 UIDs, confirming that they
are related but materially different measures and must not be substituted.

Finally, `al.rank {allianceId:Utf}` was issued for the alliance most represented in the
power cohort. It returned 95 valid UID-keyed server-1724 rows, all with the requested
alliance ID. This verifies arbitrary-opponent alliance access for that sample. The
collector still records `complete_roster=false`: the response proves an observed roster,
not completeness or pagination semantics. Raw identities and alliance IDs remain in
private VM captures only.

For Wednesday/Saturday season-battle participation, the strongest current candidate is
`lw.camp.battle.user.score.rank {}`. Static evidence proves a no-parameter response
handler but not its individual row shape, score meaning, event/round scope, retention or
opponent visibility. `lw.season.alliance.devotes.rank.info` remains a contribution
candidate, but its `day` field is an active-event subtype rather than a weekday. Neither
endpoint may be treated as attendance until a version-matched UI trace and applicable
live response establish its scope and completeness.

The matching season-2 score-list UI was subsequently traced. Opening it sends the
no-parameter command, consumes `list[]` plus `self`, sorts by `rank`, and renders
`score`, `uid`/name, `serverId` and alliance abbreviation. Its own-alliance toggle is a
client-side filter over the returned cross-alliance list. A live server-1723 request on
2026-10-13 was accepted and returned an explicit empty `list` with no error. This proves
current callability and the empty payload shape, but not last-week retention or battle
applicability. The implemented provisional metric is `season_battle_score`; empty means
no currently visible ranking, not zero participation.

## Alliance power ranking static evidence (2026-10-15)

Version-matched Lua identifies `AllianceRankList` as `alliance.rank`.
`AllianceRankListMessage` sends one integer field, `ismerge`, and passes the response to
`RankDataManager:ParseAllianceRankData`, which reads `serverRanking`.
`AllianceRankData:ParseData` maps `alliancename`, `abbr`, `fightpower`, leader, rank,
country and `serverId`/`srcServer`. This is the correct source for genuine alliance total
power; summing incomplete player-power observations is not equivalent.

No target `serverId` appears in the request shape. A request on the current server-1723
session can therefore be evaluated only for that session's ranking scope. Static evidence
does not authorise or prove arbitrary-server alliance-rank access for server 1724. The
implemented schema-v2 exporter keeps alliance IDs and `fightpower` in a specialised
alliance-entity contract; player UID records are not repurposed. It rejects source/target
server mismatch before sending any request. Live payload and coverage validation remain
pending.

The existing `PlayerRankData` path also maps `alliancename` and `abbr`. These two labels
are now allowlisted on normal `rank.get` player records so future scoped scouting captures
can display human-readable alliance names while retaining alliance ID as the join key.
