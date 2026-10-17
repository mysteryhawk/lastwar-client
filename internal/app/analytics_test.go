package app

import (
	"encoding/json"
	"lastwar-client/internal/analytics"
	"lastwar-client/internal/session"
	"lastwar-client/internal/sfs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAnalyticsExportsReadOnlySnapshots(t *testing.T) {
	c, s := session.NewPipeGameConnPair(t)
	output := filepath.Join(t.TempDir(), "private.jsonl")
	cfg := &analytics.Config{TargetServer: 1723, AllianceID: "synthetic-A", Output: output, Queries: []analytics.QueryConfig{{Kind: "thp", Period: "test-day"}, {Kind: "membership", Period: "test-day"}}}
	done := make(chan error, 1)
	go func() {
		for _, command := range []string{"rank.get", "al.rank"} {
			env, err := s.ReadEnvelope()
			if err != nil {
				done <- err
				return
			}
			m, _ := env.AsExtension()
			if m.Cmd != command {
				done <- os.ErrInvalid
				return
			}
			r := sfs.NewSFSObject()
			r.PutUtfString("uid", "9007199254740993")
			r.PutUtfString("name", "Synthetic")
			r.PutLong("heroPower", 999)
			a := sfs.NewSFSArray()
			a.AddSFSObject(r)
			p := sfs.NewSFSObject()
			if command == "rank.get" {
				p.PutSFSArray("serverRanking", a)
			} else {
				p.PutSFSArray("list", a)
			}
			p.PutInt("_id", m.Params.GetInt("_id"))
			p.PutUtfString("accessToken", "SECRET-NOT-EXPORTED")
			if err = s.SendExtension(m.Cmd, p); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	if err := runAnalytics(c, cfg, 1723); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatal("wrong snapshots")
	}
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("credential leak")
	}
	for _, line := range lines {
		var snap analytics.Snapshot
		if err = json.Unmarshal([]byte(line), &snap); err != nil {
			t.Fatal(err)
		}
		if snap.SourceServer != 1723 || snap.Status != "ok" {
			t.Fatal("bad metadata")
		}
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("output permissions")
	}
}
