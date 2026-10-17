package analytics

import (
	"context"
	"errors"
	"lastwar-client/internal/session"
	"strconv"
	"time"
)

// DiscoverMeteorite records only allowlisted scheduling fields and returns
// round ranking queries when results can exist. Lua MeteoriteActInfoData maps:
// 1 MATCH, 2 PREVIEW, 3 GRAB, 4 REST, 5 SHOW.
func (c *Collector) DiscoverMeteorite(ctx context.Context, q Query) (Snapshot, []Query, error) {
	s := Snapshot{SchemaVersion: 1, SourceServer: c.SourceServer, TargetServer: c.TargetServer,
		AllianceID: c.AllianceID, Command: q.Command, Metric: q.Metric, Period: q.Period,
		CollectedAt: time.Now().UTC(), Parameters: map[string]string{}, Records: []Record{}}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = session.DefaultCmdTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msg, err := c.Client.Request(bounded, q.Command, q.Params)
	if err != nil {
		s.Status = "transport_error"
		return s, nil, err
	}
	if msg == nil || msg.Params == nil {
		s.Status = "missing_payload"
		return s, nil, errors.New("nil meteorite context response")
	}
	p := msg.Params
	if code := p.GetString("errorCode"); code != "" {
		s.Status, s.ErrorCode = "server_error", code
		return s, nil, nil
	}
	stage, stageOK := integer(p, "stage")
	grabTimes, grabOK := integer(p, "grabTimes")
	if !stageOK || !grabOK || stage < 1 || stage > 5 || grabTimes < 0 || grabTimes > 100 {
		s.Status = "missing_payload"
		return s, nil, nil
	}
	for _, key := range []string{"beginTime", "stage", "stageEndTime", "grabTimes"} {
		if value, ok := integer(p, key); ok {
			s.Parameters[key] = strconv.FormatInt(value, 10)
		}
	}
	if begin := s.Parameters["beginTime"]; begin != "" {
		s.Period = "meteorite:" + begin
	}
	s.Status, s.PayloadPresent, s.Validation = "ok", true, "meteorite_context_response_received"
	if stage < 3 { // MATCH/PREVIEW have no completed or current grab ranking.
		return s, nil, nil
	}
	queries := make([]Query, 0, grabTimes+1)
	for round := int32(0); round <= int32(grabTimes); round++ {
		rank, buildErr := MeteoriteQuery(round, 1, 1000,
			s.Period+":round:"+strconv.FormatInt(int64(round), 10))
		if buildErr != nil {
			return s, nil, buildErr
		}
		queries = append(queries, rank)
	}
	return s, queries, nil
}
