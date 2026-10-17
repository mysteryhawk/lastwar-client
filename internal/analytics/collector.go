package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"lastwar-client/internal/session"
	"strconv"
	"time"
)

type Collector struct {
	Client                     Requester
	SourceServer, TargetServer int32
	AllianceID                 string
	Timeout                    time.Duration
	Now                        func() time.Time
}

func (c *Collector) Collect(ctx context.Context, q Query) (Snapshot, error) {
	s := Snapshot{SchemaVersion: 1, SourceServer: c.SourceServer, TargetServer: c.TargetServer, AllianceID: c.AllianceID, Command: q.Command, Metric: q.Metric, Period: q.Period, Day: q.Day, Records: []Record{}}
	s.Parameters = map[string]string{}
	for _, key := range []string{"type", "day", "rank_type", "global", "serverId", "event_id", "start", "end", "pageSize", "grabTimes", "startRank", "count"} {
		if n, ok := integer(q.Params, key); ok {
			s.Parameters[key] = strconv.FormatInt(n, 10)
		} else if key == "event_id" && q.Params.Has(key) {
			s.Parameters[key] = q.Params.GetString(key)
		}
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	s.CollectedAt = now().UTC()
	if q.Command != "al.battle.rank.info" && q.Command != "al.rank" && q.Command != "rank.get" && q.Command != "dragon.assign.player.info" && q.Command != "meteorite.rank.person.info" && q.Command != "cross.throne.person.score.rank" && q.Command != "lw.season.rank.info" && q.Command != "lw.season.alliance.devotes.rank.info" && q.Command != "lw.camp.battle.user.score.rank" {
		return s, errInvalidQuery
	}
	if c.Client == nil || c.TargetServer <= 0 || c.AllianceID == "" || q.Command == "" || q.Period == "" {
		return s, errInvalidQuery
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = session.DefaultCmdTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msg, err := c.Client.Request(bounded, q.Command, q.Params)
	if err != nil {
		s.Status = "transport_error"
		return s, err
	}
	if msg == nil {
		s.Status = "transport_error"
		return s, errors.New("nil analytics response")
	}
	s.Validation = "response_received"
	err = ParseSnapshot(&s, q, msg.Params)
	return s, err
}

// ExportJSONL emits only normalised allowlisted fields. Callers must protect
// files containing member identities; never commit production snapshots.
func ExportJSONL(w io.Writer, s Snapshot) error { return json.NewEncoder(w).Encode(s) }
