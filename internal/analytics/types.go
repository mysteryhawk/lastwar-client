// Package analytics issues only documented read-only queries and exports
// allowlisted UID-keyed observations, never arbitrary response fields.
package analytics

import (
	"context"
	"lastwar-client/internal/session"
	"lastwar-client/internal/sfs"
	"time"
)

type Requester interface {
	Request(context.Context, string, *sfs.SFSObject) (*session.ExtensionMessage, error)
}

// Snapshot is one query, including empty, incomplete and failed results. A
// ranking is never a full roster; its absence cannot establish a departure.
type Snapshot struct {
	Parameters     map[string]string `json:"parameters"`
	SchemaVersion  int               `json:"schema_version"`
	CollectedAt    time.Time         `json:"collected_at"`
	SourceServer   int32             `json:"source_server"` // verified session server, never merely target
	TargetServer   int32             `json:"target_server"`
	AllianceID     string            `json:"alliance_id"`
	Command        string            `json:"command"`
	Metric         string            `json:"metric"`
	Period         string            `json:"period"` // caller-supplied game period key; not local weekday
	Day            int32             `json:"day,omitempty"`
	Status         string            `json:"status"` // ok, empty, partial, missing_payload, server_error, transport_error
	ErrorCode      string            `json:"error_code,omitempty"`
	PayloadPresent bool              `json:"payload_present"`
	CompleteRoster bool              `json:"complete_roster"`
	Validation     string            `json:"validation"` // response_received, not proof of cross-server access
	RejectedRows   int               `json:"rejected_rows"`
	Records        []Record          `json:"records"`
}

type Record struct {
	UID          string `json:"uid"`
	Name         string `json:"name,omitempty"`
	AllianceID   string `json:"alliance_id,omitempty"`
	AllianceName string `json:"alliance_name,omitempty"`
	AllianceAbbr string `json:"alliance_abbr,omitempty"`
	ServerID     int32  `json:"server_id,omitempty"`
	Rank         *int64 `json:"rank,omitempty"`
	Value        *int64 `json:"value,omitempty"`
}

type Query struct {
	PayloadAliases                      []string
	Command                             string
	Params                              *sfs.SFSObject
	Metric, Period, Payload, ValueField string
	Day                                 int32
	Roster                              bool
	FilterAlliance                      bool
	AllianceRanking                     bool
	AllianceDetail                      bool
	EntityAllianceID                    string
}

// AllianceRankQuery follows AllianceRankListMessage. Static Lua evidence shows
// only ismerge:Int; there is no target-server dimension on this endpoint.
func AllianceRankQuery(isMerge int32, period string) (Query, error) {
	if (isMerge != 0 && isMerge != 1) || period == "" {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutInt("ismerge", isMerge)
	return Query{Command: "alliance.rank", Params: p, Metric: "alliance_power",
		Period: period, Payload: "allianceRanking", ValueField: "fightpower",
		AllianceRanking: true}, nil
}

// AllianceDetailQuery follows GetAllianceInfoMessage. Static Lua evidence
// shows one allianceId UTF string request; the response identifies its
// ownerServerId or createServer and exposes the alliance's name, member count and fightPower.
func AllianceDetailQuery(allianceID, period string) (Query, error) {
	if allianceID == "" || allianceID == "0" || len(allianceID) > 128 || period == "" {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutUtfString("allianceId", allianceID)
	return Query{Command: "get.al.info", Params: p, Metric: "alliance_detail",
		Period: period, AllianceDetail: true, EntityAllianceID: allianceID}, nil
}

// MeteoriteAutoQuery first reads the event's current schedule/stage. The app
// expands it into round rankings only when the server reports a rankable round.
func MeteoriteAutoQuery(period string) Query {
	return Query{Command: "meteorite.act.info", Params: sfs.NewSFSObject(), Metric: "meteorite_context", Period: period}
}

func VSQuery(kind, day int32, period string) (Query, error) {
	if kind < 0 || kind > 2 {
		return Query{}, errInvalidQuery
	}
	if kind == 0 && (day < 1 || day > 6) {
		return Query{}, errInvalidQuery
	}
	if kind != 0 && day != 0 {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutLong("type", int64(kind))
	if kind == 0 {
		p.PutInt("day", day)
	}
	return Query{Command: "al.battle.rank.info", Params: p, Metric: []string{"vs_daily", "vs_weekly", "vs_season"}[kind], Period: period, Payload: "rankInfo", ValueField: "score", Day: day, FilterAlliance: kind != 2}, nil
}

func RosterQuery(allianceID, period string) (Query, error) {
	if allianceID == "" || allianceID == "0" {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutUtfString("allianceId", allianceID)
	return Query{Command: "al.rank", Params: p, Metric: "membership", Period: period, Payload: "list", Roster: true}, nil
}

// BaseLevelQuery uses the read-only alliance roster response. Live upstream
// evidence and the client Lua identify mainCityLv as the member's base level;
// roster aliases are retained but the observation is not roster authority.
func BaseLevelQuery(allianceID, period string) (Query, error) {
	q, err := RosterQuery(allianceID, period)
	if err != nil {
		return Query{}, err
	}
	q.Metric = "base_level"
	q.ValueField = "mainCityLv"
	return q, nil
}

// DesertTHPQuery follows the Desert Storm participant selector. The request
// is implicitly scoped to the logged-in alliance and returns an unpaged users
// collection containing uid and heroPower, distinct from city power.
func DesertTHPQuery(period string) Query {
	return Query{Command: "dragon.assign.player.info", Params: sfs.NewSFSObject(), Metric: "THP", Period: period, Payload: "users", ValueField: "heroPower"}
}

// RankQuery follows GetRankListMessage. Numeric ranking type must come from
// Lua definitions. THP parsing requires heroPower, never total city power.
func RankQuery(rankType, global, server int32, metric, field, period string) (Query, error) {
	if rankType < 0 || (global != 0 && global != 1) || server <= 0 || metric == "" || field == "" {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutInt("type", rankType)
	p.PutInt("global", global)
	p.PutInt("serverId", server)
	return Query{Command: "rank.get", Params: p, Metric: metric, Period: period, Payload: "serverRanking", ValueField: field}, nil
}

func MeteoriteQuery(grabTimes, startRank, count int32, period string) (Query, error) {
	if grabTimes < 0 || startRank < 1 || count < 1 || count > 1000 {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutInt("grabTimes", grabTimes)
	p.PutInt("startRank", startRank)
	p.PutInt("count", count)
	return Query{Command: "meteorite.rank.person.info", Params: p, Metric: "meteorite_score", Period: period, Payload: "data", ValueField: "score"}, nil
}

// CrossKingPersonScoreRankMessage and ZoneWarManager identify this as the
// cross-war personal score list. Event applicability still needs live checking.
func SvSQuery(period string) Query {
	return Query{Command: "cross.throne.person.score.rank", Params: sfs.NewSFSObject(), Metric: "svs_score", Period: period, Payload: "ranks", ValueField: "score"}
}

// SeasonFactionBattleScoreQuery follows the season faction-war score screen.
// The endpoint returns a cross-alliance list and a separate self row. Its
// event/round retention semantics remain provisional until live validation.
func SeasonFactionBattleScoreQuery(period string) Query {
	return Query{Command: "lw.camp.battle.user.score.rank", Params: sfs.NewSFSObject(), Metric: "season_battle_score", Period: period, Payload: "list", ValueField: "score"}
}

func SeasonPowerQuery(rankType, pageSize int32, period string) (Query, error) {
	if (rankType != 2 && rankType != 4) || pageSize < 1 || pageSize > 1000 {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutInt("rank_type", rankType)
	p.PutInt("pageSize", pageSize)
	return Query{Command: "lw.season.rank.info", Params: p, Metric: "season_personal_power", Period: period, Payload: "rank", PayloadAliases: []string{"ranks", "ls"}, ValueField: "score"}, nil
}

func SeasonDevotesQuery(eventID string, day, start, end int32, period string) (Query, error) {
	if eventID == "" || day < 0 || start < 1 || (end != -1 && end < start) {
		return Query{}, errInvalidQuery
	}
	p := sfs.NewSFSObject()
	p.PutUtfString("event_id", eventID)
	p.PutInt("day", day)
	p.PutInt("start", start)
	p.PutInt("end", end)
	return Query{Command: "lw.season.alliance.devotes.rank.info", Params: p, Metric: "season_devotes_score", Period: period, Payload: "ranks", ValueField: "score"}, nil
}
