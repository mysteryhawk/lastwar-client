package analytics

import (
	"errors"
	"fmt"
	"lastwar-client/internal/sfs"
	"math"
	"strconv"
)

var errInvalidQuery = errors.New("invalid analytics query")

func integer(o *sfs.SFSObject, key string) (int64, bool) {
	v, ok := o.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.Val.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int16:
		return int64(n), true
	case byte:
		return int64(n), true
	}
	return 0, false
}

func identifier(o *sfs.SFSObject, key string) string {
	v, ok := o.Get(key)
	if !ok {
		return ""
	}
	if s, ok := v.Val.(string); ok {
		if s != "0" {
			return s
		}
		return ""
	}
	if n, ok := integer(o, key); ok && n > 0 {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// ParseSnapshot is strict: malformed/duplicate rows make the snapshot partial.
// Missing payload is distinguishable from an explicit empty array.
func ParseSnapshot(s *Snapshot, q Query, p *sfs.SFSObject) error {
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
		s.Status = "server_error"
		s.ErrorCode = code
		return nil
	}
	if v, ok := p.Get("success"); ok {
		if b, ok := v.Val.(bool); !ok || !b {
			s.Status = "server_error"
			return nil
		}
	}
	// Validate echoed query dimensions when provided; a contradictory echo
	// cannot be exported under this query's metric or game period.
	for _, key := range []string{"type", "day", "rank_type", "grabTimes", "event_id", "serverId", "global"} {
		expected, present := q.Params.Get(key)
		actual, echoed := p.Get(key)
		if !present || !echoed {
			continue
		}
		if key == "event_id" {
			if actual.Val != expected.Val {
				s.Status = "partial"
				return fmt.Errorf("mismatched %s echo", key)
			}
			continue
		}
		want, wok := integer(q.Params, key)
		got, gok := integer(p, key)
		if !wok || !gok || want != got {
			s.Status = "partial"
			return fmt.Errorf("mismatched %s echo", key)
		}
	}
	raw, ok := p.Get(q.Payload)
	if !ok {
		for _, alias := range q.PayloadAliases {
			raw, ok = p.Get(alias)
			if ok {
				break
			}
		}
	}
	if !ok {
		s.Status = "missing_payload"
		return nil
	}
	var items []sfs.SFSValue
	switch collection := raw.Val.(type) {
	case *sfs.SFSArray:
		if collection != nil {
			items = collection.Items()
			ok = true
		} else {
			ok = false
		}
	case *sfs.SFSObject:
		if collection != nil {
			for _, key := range collection.Keys() {
				v, _ := collection.Get(key)
				items = append(items, v)
			}
			ok = true
		} else {
			ok = false
		}
	default:
		ok = false
	}
	if !ok {
		s.Status = "partial"
		return fmt.Errorf("%s is not an SFS array/object", q.Payload)
	}
	s.PayloadPresent = true
	seen := map[string]bool{}
	for _, v := range items {
		o, ok := v.Val.(*sfs.SFSObject)
		if !ok || o == nil {
			s.RejectedRows++
			continue
		}
		uid := identifier(o, "uid")
		if q.Roster {
			if alias := identifier(o, "userId"); alias != "" {
				uid = alias
			}
		}
		if uid == "" || seen[uid] {
			s.RejectedRows++
			continue
		}
		seen[uid] = true
		aid := identifier(o, "aid")
		if aid == "" {
			aid = identifier(o, "allianceId")
		}
		if q.FilterAlliance {
			if aid == "" {
				s.RejectedRows++
				continue
			}
			if aid != s.AllianceID {
				continue
			}
		}
		allianceName := o.GetString("alliancename")
		if allianceName == "" {
			allianceName = o.GetString("allianceName")
		}
		allianceAbbr := o.GetString("abbr")
		if allianceAbbr == "" {
			allianceAbbr = o.GetString("allianceAbbr")
		}
		r := Record{UID: uid, Name: o.GetString("name"), AllianceID: aid,
			AllianceName: allianceName, AllianceAbbr: allianceAbbr}
		if q.Roster {
			if alias := o.GetString("playerName"); alias != "" {
				r.Name = alias
			}
			r.AllianceID = s.AllianceID
		}
		for _, key := range []string{"serverId", "srcServer"} {
			if n, ok := integer(o, key); ok && n > 0 && n <= math.MaxInt32 {
				r.ServerID = int32(n)
				break
			}
		}
		if n, ok := integer(o, "rank"); ok && n > 0 {
			r.Rank = &n
		}
		if q.ValueField != "" {
			n, ok := integer(o, q.ValueField)
			if !ok || n < 0 {
				s.RejectedRows++
				continue
			}
			r.Value = &n
		}
		s.Records = append(s.Records, r)
	}
	s.Status = "ok"
	if len(s.Records) == 0 {
		s.Status = "empty"
	}
	if s.RejectedRows > 0 {
		s.Status = "partial"
	}
	// Require a known matching session server and a nonempty fully valid roster.
	// An empty reply from an unauthorised guest must never remove every member.
	s.CompleteRoster = false // Enable only after full-roster semantics are verified live.
	return nil
}
