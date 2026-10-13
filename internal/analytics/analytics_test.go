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
	live.PutInt("srcServer", 1723)
	live.PutLong("heroPower", 123)
	s := Snapshot{}
	if err := ParseSnapshot(&s, thp, payload("serverRanking", live)); err != nil {
		t.Fatal(err)
	}
	if s.Status != "ok" || len(s.Records) != 1 || s.Records[0].AllianceID != "A" ||
		s.Records[0].ServerID != 1723 || *s.Records[0].Value != 123 {
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
	queries = append(queries, m, SvSQuery("svs:test"))
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
