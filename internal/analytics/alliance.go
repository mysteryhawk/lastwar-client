package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lastwar-client/internal/session"
	"lastwar-client/internal/sfs"
	"math"
	"sort"
	"time"
)

// AllianceSnapshot is deliberately separate from the player-UID snapshot
// contract. Alliance IDs are entity keys and must never be exported as UIDs.
type AllianceSnapshot struct {
	Parameters        map[string]string `json:"parameters"`
	SchemaVersion     int               `json:"schema_version"`
	CollectedAt       time.Time         `json:"collected_at"`
	SourceServer      int32             `json:"source_server"`
	TargetServer      int32             `json:"target_server"`
	TrackedAllianceID string            `json:"tracked_alliance_id"`
	Command           string            `json:"command"`
	Metric            string            `json:"metric"`
	Period            string            `json:"period"`
	Status            string            `json:"status"`
	ErrorCode         string            `json:"error_code,omitempty"`
	PayloadPresent    bool              `json:"payload_present"`
	CompleteCohort    bool              `json:"complete_cohort"`
	Validation        string            `json:"validation"`
	RejectedRows      int               `json:"rejected_rows"`
	ResponseFields    []string          `json:"response_fields,omitempty"`
	Alliances         []AllianceRecord  `json:"alliances"`
}

type AllianceRecord struct {
	AllianceID string `json:"alliance_id"`
	Name       string `json:"name,omitempty"`
	Abbr       string `json:"abbr,omitempty"`
	Leader     string `json:"leader,omitempty"`
	ServerID   int32  `json:"server_id,omitempty"`
	Rank       *int64 `json:"rank,omitempty"`
	TotalPower int64  `json:"total_power"`
}

func allianceIdentifier(o *sfs.SFSObject) string {
	for _, key := range []string{"allianceId", "allianceid", "aid", "id"} {
		if value := identifier(o, key); value != "" {
			return value
		}
	}
	return ""
}

func ParseAllianceSnapshot(s *AllianceSnapshot, q Query, p *sfs.SFSObject) error {
	if !q.AllianceRanking || q.Command != "alliance.rank" || q.Payload != "allianceRanking" ||
		q.ValueField != "fightpower" {
		return errInvalidQuery
	}
	if p == nil {
		s.Status = "missing_payload"
		return nil
	}
	if raw, present := p.Get("errorCode"); present {
		if _, ok := raw.Val.(string); !ok {
			s.Status = "partial"
			return errors.New("wrong-typed server errorCode")
		}
	}
	if code := p.GetString("errorCode"); code != "" {
		s.Status, s.ErrorCode = "server_error", code
		return nil
	}
	if v, ok := p.Get("success"); ok {
		if success, ok := v.Val.(bool); !ok || !success {
			s.Status = "server_error"
			return nil
		}
	}
	if _, echoed := p.Get("ismerge"); echoed {
		want, wok := integer(q.Params, "ismerge")
		got, gok := integer(p, "ismerge")
		if !wok || !gok || want != got {
			s.Status = "partial"
			return errors.New("mismatched ismerge echo")
		}
	}
	raw, present := p.Get(q.Payload)
	if !present {
		for _, key := range p.Keys() {
			if !sfs.IsSensitiveSFSKey(key) {
				s.ResponseFields = append(s.ResponseFields, key)
			}
		}
		sort.Strings(s.ResponseFields)
		s.Status = "missing_payload"
		return nil
	}
	var items []sfs.SFSValue
	switch collection := raw.Val.(type) {
	case *sfs.SFSArray:
		if collection == nil {
			s.Status = "partial"
			return errors.New("nil alliance ranking array")
		}
		items = collection.Items()
	case *sfs.SFSObject:
		if collection == nil {
			s.Status = "partial"
			return errors.New("nil alliance ranking object")
		}
		for _, key := range collection.Keys() {
			value, _ := collection.Get(key)
			items = append(items, value)
		}
	default:
		s.Status = "partial"
		return fmt.Errorf("%s is not an SFS array/object", q.Payload)
	}
	s.PayloadPresent = true
	seen := map[string]bool{}
	for _, value := range items {
		row, ok := value.Val.(*sfs.SFSObject)
		if !ok || row == nil {
			s.RejectedRows++
			continue
		}
		allianceID := allianceIdentifier(row)
		power, powerOK := integer(row, "fightpower")
		if allianceID == "" || seen[allianceID] || !powerOK || power < 0 {
			s.RejectedRows++
			continue
		}
		seen[allianceID] = true
		record := AllianceRecord{AllianceID: allianceID, Name: row.GetString("alliancename"),
			Abbr: row.GetString("abbr"), Leader: row.GetString("leader"), TotalPower: power}
		if record.Name == "" {
			record.Name = row.GetString("allianceName")
		}
		for _, key := range []string{"serverId", "srcServer"} {
			if n, ok := integer(row, key); ok && n > 0 && n <= math.MaxInt32 {
				record.ServerID = int32(n)
				break
			}
		}
		if record.ServerID != 0 && record.ServerID != s.TargetServer {
			s.RejectedRows++
			continue
		}
		if rank, ok := integer(row, "rank"); ok && rank > 0 {
			record.Rank = &rank
		}
		s.Alliances = append(s.Alliances, record)
	}
	s.Status = "ok"
	if len(s.Alliances) == 0 {
		s.Status = "empty"
	}
	if s.RejectedRows > 0 {
		s.Status = "partial"
	}
	s.CompleteCohort = false
	return nil
}

func (c *Collector) CollectAllianceRanking(ctx context.Context, q Query) (AllianceSnapshot, error) {
	s := AllianceSnapshot{SchemaVersion: 2, SourceServer: c.SourceServer,
		TargetServer: c.TargetServer, TrackedAllianceID: c.AllianceID,
		Command: q.Command, Metric: q.Metric, Period: q.Period,
		Parameters: map[string]string{}, Alliances: []AllianceRecord{}}
	if isMerge, ok := integer(q.Params, "ismerge"); ok {
		s.Parameters["ismerge"] = fmt.Sprintf("%d", isMerge)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	s.CollectedAt = now().UTC()
	if c.Client == nil || c.SourceServer <= 0 || c.SourceServer != c.TargetServer ||
		c.AllianceID == "" || !q.AllianceRanking || q.Command != "alliance.rank" || q.Period == "" {
		s.Status = "transport_error"
		return s, errInvalidQuery
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = session.DefaultCmdTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	message, err := c.Client.Request(bounded, q.Command, q.Params)
	if err != nil {
		s.Status = "transport_error"
		return s, err
	}
	if message == nil {
		s.Status = "transport_error"
		return s, errors.New("nil alliance ranking response")
	}
	s.Validation = "response_received_same_server"
	err = ParseAllianceSnapshot(&s, q, message.Params)
	return s, err
}

func ExportAllianceJSONL(w io.Writer, s AllianceSnapshot) error {
	return json.NewEncoder(w).Encode(s)
}
