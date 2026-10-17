package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"lastwar-client/internal/session"
	"lastwar-client/internal/sfs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func row(uid, aid string, value int64) *sfs.SFSObject {
	r := sfs.NewSFSObject()
	r.PutUtfString("uid", uid)
	r.PutUtfString("aid", aid)
	r.PutUtfString("name", "Synthetic")
	r.PutLong("score", value)
	r.PutLong("heroPower", value)
	r.PutLong("power", value+123)
	return r
}
func payload(key string, rows ...*sfs.SFSObject) *sfs.SFSObject {
	p := sfs.NewSFSObject()
	a := sfs.NewSFSArray()
	for _, r := range rows {
		a.AddSFSObject(r)
	}
	p.PutSFSArray(key, a)
	return p
}

type staticRequester struct{ params *sfs.SFSObject }

func (r staticRequester) Request(context.Context, string, *sfs.SFSObject) (*session.ExtensionMessage, error) {
	return &session.ExtensionMessage{Params: r.params}, nil
}

type countingRequester struct {
	params *sfs.SFSObject
	calls  int
}

func (r *countingRequester) Request(context.Context, string, *sfs.SFSObject) (*session.ExtensionMessage, error) {
	r.calls++
	return &session.ExtensionMessage{Params: r.params}, nil
}

func TestMeteoriteAutoDiscoveryUsesLiveStageAndRounds(t *testing.T) {
	p := sfs.NewSFSObject()
	p.PutInt("stage", 4)
	p.PutInt("grabTimes", 2)
	p.PutLong("beginTime", 100)
	p.PutLong("stageEndTime", 200)
	c := Collector{Client: staticRequester{p}, SourceServer: 1723, TargetServer: 1723, AllianceID: "A"}
	s, queries, err := c.DiscoverMeteorite(context.Background(), MeteoriteAutoQuery("meteorite:event"))
	if err != nil || s.Status != "ok" || s.Parameters["stage"] != "4" || len(queries) != 3 {
		t.Fatalf("snapshot=%+v queries=%d err=%v", s, len(queries), err)
	}
	for i, q := range queries {
		if q.Params.GetInt("grabTimes") != int32(i) || q.Params.GetInt("count") != 1000 {
			t.Fatalf("round %d = %v", i, q.Params)
		}
	}
	p.PutInt("stage", 2)
	_, queries, err = c.DiscoverMeteorite(context.Background(), MeteoriteAutoQuery("meteorite:event"))
	if err != nil || len(queries) != 0 {
		t.Fatal("preview stage must not guess rankings")
	}
}

func TestQueriesHaveLuaWireTags(t *testing.T) {
	for kind := int32(0); kind < 3; kind++ {
		day := int32(0)
		if kind == 0 {
			day = 6
		}
		q, e := VSQuery(kind, day, "test-period")
		if e != nil {
			t.Fatal(e)
		}
		v, _ := q.Params.Get("type")
		if v.Type != sfs.SFSLong {
			t.Fatal("VS type not Long")
		}
		if kind == 0 {
			v, _ = q.Params.Get("day")
			if v.Type != sfs.SFSInt {
				t.Fatal("day not Int")
			}
		} else if q.Params.Has("day") {
			t.Fatal("unexpected day")
		}
	}
	for _, pair := range [][2]int32{{0, 0}, {0, 7}, {1, 1}, {3, 0}} {
		if _, e := VSQuery(pair[0], pair[1], "p"); e == nil {
			t.Fatal("invalid query accepted")
		}
	}
	q, e := RankQuery(THPRankingType, 0, 1723, "THP", "heroPower", "p")
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"type", "global", "serverId"} {
		v, _ := q.Params.Get(key)
		if v.Type != sfs.SFSInt {
			t.Fatal("ranking param not Int")
		}
	}
	if q.Params.GetInt("type") != 13 {
		t.Fatal("wrong THP constant")
	}
	power, e := RankQuery(PlayerPowerRankingType, 0, 1724, "player_power", "power", "p")
	if e != nil || power.Params.GetInt("type") != 4 || power.ValueField != "power" {
		t.Fatal("wrong player power query")
	}
	battle := SeasonFactionBattleScoreQuery("season-battle:test")
	if battle.Command != "lw.camp.battle.user.score.rank" || battle.Payload != "list" || battle.ValueField != "score" {
		t.Fatal("wrong season battle score query")
	}
	alliance, e := AllianceRankQuery(0, "day:test")
	if e != nil || alliance.Command != "alliance.rank" || alliance.Payload != "allianceRanking" || !alliance.AllianceRanking ||
		alliance.Params.GetInt("ismerge") != 0 {
		t.Fatal("wrong alliance power query")
	}
	if value, _ := alliance.Params.Get("ismerge"); value.Type != sfs.SFSInt {
		t.Fatal("alliance ismerge not Int")
	}
	if _, e := AllianceRankQuery(2, "day:test"); e == nil {
		t.Fatal("invalid alliance merge scope accepted")
	}
	detail, e := AllianceDetailQuery("alliance-1724-A", "day:test")
	if e != nil || detail.Command != "get.al.info" || !detail.AllianceDetail ||
		detail.Params.GetString("allianceId") != "alliance-1724-A" {
		t.Fatal("wrong alliance detail query")
	}
	if _, e := AllianceDetailQuery("", "day:test"); e == nil {
		t.Fatal("empty alliance detail identity accepted")
	}
}

func TestAllianceDetailValidatesForeignOwnerServerAndMetrics(t *testing.T) {
	q, err := AllianceDetailQuery("alliance-1724-A", "game-day:2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	p := sfs.NewSFSObject()
	p.PutUtfString("uid", "alliance-1724-A")
	p.PutUtfString("name", "Foreign Alliance")
	p.PutUtfString("abbr", "FOR")
	p.PutUtfString("leaderName", "Leader")
	p.PutInt("ownerServerId", 1724)
	p.PutInt("curMember", 97)
	p.PutLong("fightPower", 9876543210)
	requester := &countingRequester{params: p}
	collector := Collector{Client: requester, SourceServer: 1723, TargetServer: 1724,
		AllianceID: "tracked-doac"}
	snapshot, err := collector.CollectAllianceRanking(context.Background(), q)
	if err != nil || snapshot.Status != "ok" || snapshot.Validation != "response_owner_server_match" ||
		len(snapshot.Alliances) != 1 || snapshot.Alliances[0].Name != "Foreign Alliance" ||
		snapshot.Alliances[0].ServerID != 1724 || snapshot.Alliances[0].MemberCount != 97 ||
		snapshot.Alliances[0].TotalPower != 9876543210 {
		t.Fatalf("alliance detail snapshot=%+v err=%v", snapshot, err)
	}

	p.PutInt("ownerServerId", 1723)
	snapshot, err = collector.CollectAllianceRanking(context.Background(), q)
	if err == nil || snapshot.Status != "partial" || len(snapshot.Alliances) != 0 {
		t.Fatalf("foreign owner mismatch accepted: snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.RejectionReasons["server_mismatch"] != 1 ||
		!reflect.DeepEqual(snapshot.ResponseFields, []string{"abbr", "curMember", "fightPower", "leaderName", "name", "ownerServerId", "uid"}) {
		t.Fatalf("unsafe or incomplete detail diagnostic: %+v", snapshot)
	}
	createServerOnly := sfs.NewSFSObject()
	createServerOnly.PutUtfString("uid", "alliance-1724-A")
	createServerOnly.PutUtfString("name", "Foreign Alliance")
	createServerOnly.PutInt("createServer", 1724)
	createServerOnly.PutInt("curMember", 97)
	createServerOnly.PutLong("fightPower", 9876543210)
	requester.params = createServerOnly
	snapshot, err = collector.CollectAllianceRanking(context.Background(), q)
	if err != nil || snapshot.Status != "ok" || snapshot.Validation != "response_create_server_match" ||
		snapshot.Alliances[0].ServerID != 1724 {
		t.Fatalf("createServer fallback rejected: snapshot=%+v err=%v", snapshot, err)
	}
	createServerOnly.PutInt("ownerServerId", 1723)
	snapshot, err = collector.CollectAllianceRanking(context.Background(), q)
	if err == nil || snapshot.RejectionReasons["conflicting_server_ids"] != 1 {
		t.Fatalf("conflicting server identities accepted: snapshot=%+v err=%v", snapshot, err)
	}
	alias := sfs.NewSFSObject()
	alias.PutUtfString("uid", "alliance-1724-A")
	alias.PutUtfString("name", "Foreign Alliance")
	alias.PutInt("ownerServerId", 1724)
	alias.PutInt("memberCount", 97)
	alias.PutLong("fightPower", 9876543210)
	requester.params = alias
	snapshot, err = collector.CollectAllianceRanking(context.Background(), q)
	if err != nil || snapshot.Status != "ok" || snapshot.Alliances[0].MemberCount != 97 {
		t.Fatalf("memberCount alias rejected: snapshot=%+v err=%v", snapshot, err)
	}
}

func TestAllianceRankingHasSeparateEntityContractAndSameServerGuard(t *testing.T) {
	q, err := AllianceRankQuery(0, "game-day:2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	row := sfs.NewSFSObject()
	row.PutUtfString("uid", "alliance-A")
	row.PutUtfString("alliancename", "Alliance Alpha")
	row.PutUtfString("abbr", "ALP")
	row.PutUtfString("leader", "Leader")
	row.PutInt("serverId", 1723)
	row.PutInt("rank", 2)
	row.PutLong("fightpower", 123456789)
	requester := &countingRequester{params: payload("allianceRanking", row)}
	now := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)
	collector := Collector{Client: requester, SourceServer: 1723, TargetServer: 1723,
		AllianceID: "alliance-A", Now: func() time.Time { return now }}
	snapshot, err := collector.CollectAllianceRanking(context.Background(), q)
	if err != nil || snapshot.SchemaVersion != 2 || snapshot.Status != "ok" ||
		len(snapshot.Alliances) != 1 || snapshot.Alliances[0].AllianceID != "alliance-A" ||
		snapshot.Alliances[0].TotalPower != 123456789 || snapshot.CompleteCohort ||
		snapshot.Alliances[0].Rank == nil || *snapshot.Alliances[0].Rank != 1 {
		t.Fatalf("alliance snapshot=%+v err=%v", snapshot, err)
	}
	var exported bytes.Buffer
	if err := ExportAllianceJSONL(&exported, snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported.String(), `"uid"`) || !strings.Contains(exported.String(), `"total_power"`) {
		t.Fatalf("alliance export mixed with player contract: %s", exported.String())
	}
	for _, diagnostic := range []string{`"row_shape"`, `"self_ranking_type"`,
		`"response_fields"`, `"rejection_reasons"`} {
		if strings.Contains(exported.String(), diagnostic) {
			t.Fatalf("successful operational export contains diagnostic field %s: %s",
				diagnostic, exported.String())
		}
	}
	requester.calls = 0
	collector.SourceServer = 1456
	rejected, err := collector.CollectAllianceRanking(context.Background(), q)
	if !errors.Is(err, errInvalidQuery) || requester.calls != 0 || rejected.Status != "transport_error" {
		t.Fatal("cross-server alliance request was not stopped before send")
	}
}

func TestAllianceRankingMissingPayloadRecordsFieldNamesOnly(t *testing.T) {
	q, err := AllianceRankQuery(0, "game-day:2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	p := sfs.NewSFSObject()
	p.PutInt("_id", 4)
	p.PutSFSArray("serverRanking", sfs.NewSFSArray())
	p.PutUtfString("accessToken", "SECRET")
	s := AllianceSnapshot{}
	if err := ParseAllianceSnapshot(&s, q, p); err != nil {
		t.Fatal(err)
	}
	if s.Status != "missing_payload" || s.ResponseOtherFields != 1 ||
		!reflect.DeepEqual(s.ResponseFields, []string{"_id", "serverRanking"}) {
		t.Fatalf("unexpected diagnostic: %+v", s)
	}
	exported := new(bytes.Buffer)
	if err := ExportAllianceJSONL(exported, s); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported.String(), "accessToken") || strings.Contains(exported.String(), "SECRET") {
		t.Fatalf("diagnostic exported values: %s", exported.String())
	}
}

func TestAllianceRankingRejectedRowShapeIsBoundedAndValueFree(t *testing.T) {
	q, err := AllianceRankQuery(0, "game-day:2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	row := sfs.NewSFSObject()
	row.PutUtfString("name", "VISIBLE-NAME-SENTINEL")
	row.PutLong("fightpower", 987654321)
	row.PutUtfString("mystery", "MYSTERY-VALUE-SENTINEL")
	row.PutUtfString("accessToken", "SECRET-TOKEN-SENTINEL")
	s := AllianceSnapshot{}
	p := payload("allianceRanking", row)
	p.PutInt("selfRanking", 7)
	if err := ParseAllianceSnapshot(&s, q, p); err != nil {
		t.Fatal(err)
	}
	wantFields := []string{"fightpower:long", "name:utf_string"}
	if s.Status != "partial" || s.RejectedRows != 1 ||
		s.RejectionReasons["missing_alliance_id"] != 1 || s.RowShape == nil ||
		s.SelfRankingType != "int" || s.RowShape.CollectionType != "sfs_array" ||
		s.RowShape.ItemCount != 1 || s.RowShape.RowsExamined != 1 ||
		s.RowShape.ItemTypes["sfs_object"] != 1 || len(s.RowShape.Shapes) != 1 ||
		s.RowShape.Shapes[0].Count != 1 || s.RowShape.Shapes[0].OtherFields != 2 ||
		!reflect.DeepEqual(s.RowShape.Shapes[0].Fields, wantFields) {
		t.Fatalf("unexpected row diagnostic: %+v", s)
	}
	exported := new(bytes.Buffer)
	if err := ExportAllianceJSONL(exported, s); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"VISIBLE-NAME-SENTINEL", "987654321", "mystery",
		"MYSTERY-VALUE-SENTINEL", "accessToken", "SECRET-TOKEN-SENTINEL"} {
		if strings.Contains(exported.String(), forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, exported.String())
		}
	}
}

func TestAllianceRankingRowShapeCapsSamples(t *testing.T) {
	q, _ := AllianceRankQuery(0, "game-day:2026-10-16")
	rows := make([]*sfs.SFSObject, 9)
	for i := range rows {
		rows[i] = sfs.NewSFSObject()
		rows[i].PutLong("fightpower", int64(i+1))
	}
	s := AllianceSnapshot{}
	if err := ParseAllianceSnapshot(&s, q, payload("allianceRanking", rows...)); err != nil {
		t.Fatal(err)
	}
	if s.RowShape == nil || s.RowShape.ItemCount != 9 || s.RowShape.RowsExamined != 8 ||
		!s.RowShape.Truncated || s.RejectedRows != 9 {
		t.Fatalf("unbounded row diagnostic: %+v", s)
	}
}

func TestParseRankingStrictUIDValueAndAlliance(t *testing.T) {
	q, _ := VSQuery(0, 1, "p")
	s := Snapshot{AllianceID: "A", SourceServer: 1723, TargetServer: 1723}
	big := row("9007199254740993", "A", 9223372036854775807)
	p := payload("rankInfo", big, row("enemy", "B", 100), row("9007199254740993", "A", 12), row("missing", "", 3))
	if e := ParseSnapshot(&s, q, p); e != nil {
		t.Fatal(e)
	}
	if s.Status != "partial" || len(s.Records) != 1 || s.RejectedRows != 2 || *s.Records[0].Value != 9223372036854775807 {
		t.Fatalf("status %s rows %d rejected %d", s.Status, len(s.Records), s.RejectedRows)
	}
	if s.CompleteRoster {
		t.Fatal("ranking marked roster")
	}
	thp, _ := RankQuery(13, 0, 1723, "THP", "heroPower", "p")
	s = Snapshot{}
	if e := ParseSnapshot(&s, thp, payload("serverRanking", row("p", "A", 1000))); e != nil {
		t.Fatal(e)
	}
	if *s.Records[0].Value != 1000 {
		t.Fatal("city power substituted for THP")
	}
	bad := row("p", "A", 0)
	bad.PutDouble("heroPower", 100.0)
	s = Snapshot{}
	_ = ParseSnapshot(&s, thp, payload("serverRanking", bad))
	if s.Status != "partial" || len(s.Records) != 0 {
		t.Fatal("coerced double score")
	}
}

func TestLiveObservedTHPAndEmptyVSShapes(t *testing.T) {
	thp, _ := RankQuery(THPRankingType, 0, 1723, "THP", "heroPower", "p")
	live := sfs.NewSFSObject()
	live.PutUtfString("uid", "synthetic-uid")
	live.PutUtfString("allianceId", "A")
	live.PutUtfString("alliancename", "Alliance Alpha")
	live.PutUtfString("abbr", "ALP")
	live.PutInt("srcServer", 1723)
	live.PutLong("heroPower", 123)
	s := Snapshot{}
	if err := ParseSnapshot(&s, thp, payload("serverRanking", live)); err != nil {
		t.Fatal(err)
	}
	if s.Status != "ok" || len(s.Records) != 1 || s.Records[0].AllianceID != "A" ||
		s.Records[0].ServerID != 1723 || s.Records[0].AllianceName != "Alliance Alpha" ||
		s.Records[0].AllianceAbbr != "ALP" || *s.Records[0].Value != 123 {
		t.Fatalf("live THP aliases not preserved: %+v", s)
	}
	for kind, metric := range []string{"vs_daily", "vs_weekly", "vs_season"} {
		day := int32(0)
		if kind == 0 {
			day = 1
		}
		q, _ := VSQuery(int32(kind), day, "p")
		for _, empty := range []*sfs.SFSObject{payload("rankInfo"), func() *sfs.SFSObject {
			p, table := sfs.NewSFSObject(), sfs.NewSFSObject()
			p.PutSFSObject("rankInfo", table)
			return p
		}()} {
			s = Snapshot{AllianceID: "A"}
			if err := ParseSnapshot(&s, q, empty); err != nil || s.Status != "empty" ||
				!s.PayloadPresent || len(s.Records) != 0 {
				t.Fatalf("%s empty live shape: status=%s present=%v rows=%d err=%v",
					metric, s.Status, s.PayloadPresent, len(s.Records), err)
			}
		}
	}
}

func TestDesertTHPUsesUnpagedUsersAndHeroPower(t *testing.T) {
	q := DesertTHPQuery("storm:test")
	if q.Command != "dragon.assign.player.info" || len(q.Params.Keys()) != 0 || q.Payload != "users" {
		t.Fatalf("wrong Desert THP wire shape: %+v", q)
	}
	r := sfs.NewSFSObject()
	r.PutUtfString("uid", "synthetic-uid")
	r.PutUtfString("name", "Synthetic")
	r.PutLong("heroPower", 123)
	r.PutLong("power", 999)
	r.PutLong("armyPower", 456)
	s := Snapshot{AllianceID: "A", SourceServer: 1723, TargetServer: 1723}
	if err := ParseSnapshot(&s, q, payload("users", r)); err != nil {
		t.Fatal(err)
	}
	if s.Status != "ok" || len(s.Records) != 1 || s.Records[0].UID != "synthetic-uid" ||
		s.Records[0].Value == nil || *s.Records[0].Value != 123 {
		t.Fatalf("Desert THP parse = %+v", s)
	}
	missing := sfs.NewSFSObject()
	missing.PutUtfString("uid", "city-power-only")
	missing.PutLong("power", 999)
	s = Snapshot{AllianceID: "A", SourceServer: 1723, TargetServer: 1723}
	_ = ParseSnapshot(&s, q, payload("users", missing))
	if s.Status != "partial" || len(s.Records) != 0 {
		t.Fatal("city power substituted for Desert THP")
	}
}

func TestMissingEmptyErrorsAndRosterIdentity(t *testing.T) {
	q, _ := RosterQuery("A", "p")
	for _, tc := range []struct {
		p      *sfs.SFSObject
		status string
	}{{nil, "missing_payload"}, {sfs.NewSFSObject(), "missing_payload"}, {payload("list"), "empty"}} {
		s := Snapshot{}
		_ = ParseSnapshot(&s, q, tc.p)
		if s.Status != tc.status || s.CompleteRoster {
			t.Fatal("empty roster authoritative")
		}
	}
	p := sfs.NewSFSObject()
	p.PutUtfString("errorCode", "E011")
	s := Snapshot{}
	_ = ParseSnapshot(&s, q, p)
	if s.Status != "server_error" || s.ErrorCode != "E011" {
		t.Fatal("error ignored")
	}
	r := row("old", "", 0)
	r.PutUtfString("userId", "stable")
	s = Snapshot{AllianceID: "A", SourceServer: 1723, TargetServer: 1723}
	_ = ParseSnapshot(&s, q, payload("list", r))
	if s.Records[0].UID != "stable" || s.Records[0].AllianceID != "A" || s.CompleteRoster {
		t.Fatal("bad roster identity/authority")
	}
	p = sfs.NewSFSObject()
	p.PutUtfString("list", "unexpected")
	s = Snapshot{}
	if e := ParseSnapshot(&s, q, p); e == nil || s.Status != "partial" {
		t.Fatal("malformed payload accepted")
	}
}

func TestBaseLevelUsesRosterAliasesWithoutRosterAuthority(t *testing.T) {
	q, err := BaseLevelQuery("A", "day:test")
	if err != nil {
		t.Fatal(err)
	}
	r := sfs.NewSFSObject()
	r.PutUtfString("uid", "legacy")
	r.PutUtfString("userId", "stable")
	r.PutUtfString("playerName", "Synthetic")
	r.PutInt("mainCityLv", 31)
	s := Snapshot{AllianceID: "A", SourceServer: 1723, TargetServer: 1723}
	if err := ParseSnapshot(&s, q, payload("list", r)); err != nil {
		t.Fatal(err)
	}
	if s.Status != "ok" || len(s.Records) != 1 || s.Records[0].UID != "stable" ||
		s.Records[0].Name != "Synthetic" || s.Records[0].Value == nil ||
		*s.Records[0].Value != 31 || s.CompleteRoster {
		t.Fatalf("base level parse = %+v", s)
	}
}

func TestCollectorRoundTripAndExportAllowlist(t *testing.T) {
	c, server := session.NewPipeGameConnPair(t)
	d := c.StartDispatcher()
	defer func() { _ = d.Close() }()
	go func() {
		env, e := server.ReadEnvelope()
		if e != nil {
			return
		}
		m, _ := env.AsExtension()
		p := payload("rankInfo", row("9007199254740993", "A", 123))
		p.PutInt("_id", m.Params.GetInt("_id"))
		p.PutUtfString("accessToken", "SECRET-NOT-EXPORTED")
		_ = server.SendExtension(m.Cmd, p)
	}()
	q, _ := VSQuery(1, 0, "week:test")
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	collector := Collector{Client: d, SourceServer: 1456, TargetServer: 1723, AllianceID: "A", Now: func() time.Time { return now }}
	s, e := collector.Collect(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	if s.SourceServer != 1456 || s.TargetServer != 1723 || s.Status != "ok" || len(s.Records) != 1 {
		t.Fatal("wrong snapshot metadata")
	}
	var b bytes.Buffer
	if e := ExportJSONL(&b, s); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(b.String(), "SECRET") || strings.Contains(b.String(), "accessToken") {
		t.Fatal("raw credentials exported")
	}
	var decoded Snapshot
	if e := json.Unmarshal(bytes.TrimSpace(b.Bytes()), &decoded); e != nil {
		t.Fatal(e)
	}
	if decoded.Records[0].UID != "9007199254740993" || !decoded.CollectedAt.Equal(now) {
		t.Fatal("roundtrip identity/time")
	}
	q.Command = "building.production.collect"
	if _, e = collector.Collect(context.Background(), q); !errors.Is(e, errInvalidQuery) {
		t.Fatal("gameplay operation accepted")
	}
}

func TestAnalyticsConfigRejectsUnknownOperations(t *testing.T) {
	for _, tc := range []struct {
		json  string
		valid bool
	}{
		{`{"target_server":1723,"alliance_id":"synthetic","output":"snapshots.jsonl","queries":[{"kind":"vs_daily","period":"test-day","day":1},{"kind":"thp","period":"test-day"}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"base_level","period":"test-day"}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"collect","period":"p"}]}`, false},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"vs_daily","period":"p","day":7}]}`, false},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"meteorite","period":"p","grab_times":0}]}`, true},
		{`{"target_server":1724,"alliance_id":"synthetic","output":"s","queries":[{"kind":"player_power","period":"p"}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"alliance_power","period":"p","ismerge":0}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"alliance_power","period":"p","ismerge":2}]}`, false},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"season_battle_score","period":"p"}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"desert_thp","period":"p"}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"meteorite","period":"p"}]}`, false},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"season_devotes","period":"p","event_id":"event","sub_type":0}]}`, true},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"season_devotes","period":"p","event_id":"event"}]}`, false},
		{`{"target_server":1723,"alliance_id":"synthetic","output":"s","queries":[{"kind":"thp","period":"p"}],"accessToken":"secret"}`, false},
	} {
		p := filepath.Join(t.TempDir(), "config.json")
		if e := os.WriteFile(p, []byte(tc.json), 0600); e != nil {
			t.Fatal(e)
		}
		_, e := LoadConfig(p)
		if (e == nil) != tc.valid {
			t.Fatalf("config validity %v error %v", tc.valid, e)
		}
	}
}

func TestAdditionalRankingQueriesAndEchoDimensions(t *testing.T) {
	queries := []Query{}
	m, e := MeteoriteQuery(0, 1, 100, "meteorite:test")
	if e != nil {
		t.Fatal(e)
	}
	queries = append(queries, m, SvSQuery("svs:test"), SeasonFactionBattleScoreQuery("season-battle:test"))
	season, e := SeasonPowerQuery(2, 100, "season:test")
	if e != nil {
		t.Fatal(e)
	}
	queries = append(queries, season)
	devotes, e := SeasonDevotesQuery("synthetic-event", 0, 1, -1, "season:test")
	if e != nil {
		t.Fatal(e)
	}
	queries = append(queries, devotes)
	for _, q := range queries {
		p := payload(q.Payload, row("1", "A", 123))
		s := Snapshot{}
		if e := ParseSnapshot(&s, q, p); e != nil || s.Status != "ok" || len(s.Records) != 1 {
			t.Fatalf("%s parse %v", q.Command, e)
		}
	}
	s := Snapshot{}
	p := payload("ranks", row("1", "A", 123))
	if e := ParseSnapshot(&s, season, p); e != nil || s.Status != "ok" {
		t.Fatal("season alias")
	}
	q, _ := VSQuery(0, 1, "p")
	p = payload("rankInfo", row("1", "A", 1))
	p.PutInt("day", 6)
	s = Snapshot{AllianceID: "A"}
	if e := ParseSnapshot(&s, q, p); e == nil || len(s.Records) != 0 {
		t.Fatal("contradictory day exported")
	}
	mapped := sfs.NewSFSObject()
	mapped.PutSFSObject("first", row("1", "A", 123))
	p = sfs.NewSFSObject()
	p.PutSFSObject("rankInfo", mapped)
	s = Snapshot{AllianceID: "A"}
	if e := ParseSnapshot(&s, q, p); e != nil || s.Status != "ok" {
		t.Fatal("Lua table collection")
	}
	if _, e = SeasonPowerQuery(1, 100, "p"); e == nil {
		t.Fatal("alliance UID rank accepted as personal")
	}
}
