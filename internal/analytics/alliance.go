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
	"strings"
	"time"
)

// AllianceSnapshot is deliberately separate from the player-UID snapshot
// contract. Alliance IDs are entity keys and must never be exported as UIDs.
type AllianceSnapshot struct {
	Parameters          map[string]string `json:"parameters"`
	SchemaVersion       int               `json:"schema_version"`
	CollectedAt         time.Time         `json:"collected_at"`
	SourceServer        int32             `json:"source_server"`
	TargetServer        int32             `json:"target_server"`
	TrackedAllianceID   string            `json:"tracked_alliance_id"`
	Command             string            `json:"command"`
	Metric              string            `json:"metric"`
	Period              string            `json:"period"`
	Status              string            `json:"status"`
	ErrorCode           string            `json:"error_code,omitempty"`
	PayloadPresent      bool              `json:"payload_present"`
	CompleteCohort      bool              `json:"complete_cohort"`
	Validation          string            `json:"validation"`
	RejectedRows        int               `json:"rejected_rows"`
	ResponseFields      []string          `json:"response_fields,omitempty"`
	ResponseOtherFields int               `json:"response_other_fields,omitempty"`
	SelfRankingType     string            `json:"self_ranking_type,omitempty"`
	RejectionReasons    map[string]int    `json:"rejection_reasons,omitempty"`
	RowShape            *AllianceRowShape `json:"row_shape,omitempty"`
	Alliances           []AllianceRecord  `json:"alliances"`
}

// AllianceRowShape is a bounded, value-free live-protocol diagnostic. It emits
// only reviewed field names, SFS wire types and aggregate counts. It must never
// contain scalar values, arbitrary field names or nested payload contents.
type AllianceRowShape struct {
	CollectionType string                    `json:"collection_type"`
	ItemCount      int                       `json:"item_count"`
	RowsExamined   int                       `json:"rows_examined"`
	ItemTypes      map[string]int            `json:"item_types"`
	Shapes         []AllianceRowShapeVariant `json:"shapes"`
	Truncated      bool                      `json:"truncated,omitempty"`
}

type AllianceRowShapeVariant struct {
	Fields      []string `json:"fields"`
	OtherFields int      `json:"other_fields,omitempty"`
	Count       int      `json:"count"`
}

type AllianceRecord struct {
	AllianceID  string `json:"alliance_id"`
	Name        string `json:"name,omitempty"`
	Abbr        string `json:"abbr,omitempty"`
	Leader      string `json:"leader,omitempty"`
	ServerID    int32  `json:"server_id,omitempty"`
	Rank        *int64 `json:"rank,omitempty"`
	TotalPower  int64  `json:"total_power"`
	MemberCount int64  `json:"member_count,omitempty"`
}

func allianceIdentifier(o *sfs.SFSObject) string {
	for _, key := range []string{"uid", "allianceId", "allianceid", "aid", "id"} {
		if value := identifier(o, key); value != "" {
			return value
		}
	}
	return ""
}

var allianceShapeFields = map[string]bool{
	"abbr": true, "aid": true, "allianceId": true,
	"allianceName": true, "allianceid": true, "alliancename": true,
	"armyKill": true, "country": true, "fightpower": true, "icon": true,
	"id": true, "leader": true, "name": true, "rank": true, "score": true,
	"serverId": true, "srcServer": true, "uid": true,
}

var allianceResponseFields = map[string]bool{
	"_id": true, "_time": true, "allianceRanking": true, "errorCode": true,
	"ismerge": true, "selfRanking": true, "serverRanking": true, "success": true,
}

func sfsTypeName(tag byte) string {
	switch tag {
	case sfs.SFSNull:
		return "null"
	case sfs.SFSBool:
		return "bool"
	case 2:
		return "byte"
	case 3:
		return "short"
	case sfs.SFSInt:
		return "int"
	case sfs.SFSLong:
		return "long"
	case 6:
		return "float"
	case sfs.SFSDouble:
		return "double"
	case sfs.SFSUtfString:
		return "utf_string"
	case 9, 10, 11, 12, 13, 14, 15, 16:
		return "primitive_array"
	case 17:
		return "sfs_array"
	case sfs.SFSObjectType:
		return "sfs_object"
	case 19:
		return "class"
	case sfs.SFSText:
		return "text"
	default:
		return "unknown"
	}
}

func allianceRowShape(raw sfs.SFSValue, items []sfs.SFSValue) *AllianceRowShape {
	const maxRows, maxFields, maxVariants = 8, 18, 8
	d := &AllianceRowShape{CollectionType: sfsTypeName(raw.Type), ItemCount: len(items),
		ItemTypes: map[string]int{}, Shapes: []AllianceRowShapeVariant{}}
	variants := map[string]AllianceRowShapeVariant{}
	limit := len(items)
	if limit > maxRows {
		limit, d.Truncated = maxRows, true
	}
	d.RowsExamined = limit
	for _, item := range items[:limit] {
		typeName := sfsTypeName(item.Type)
		d.ItemTypes[typeName]++
		row, ok := item.Val.(*sfs.SFSObject)
		if !ok || row == nil {
			continue
		}
		fields := make([]string, 0, len(row.Keys()))
		other := 0
		for _, key := range row.Keys() {
			if sfs.IsSensitiveSFSKey(key) {
				other++
				continue
			}
			value, _ := row.Get(key)
			if !allianceShapeFields[key] || len(fields) >= maxFields {
				other++
				continue
			}
			fields = append(fields, key+":"+sfsTypeName(value.Type))
		}
		sort.Strings(fields)
		signature := strings.Join(fields, "|") + fmt.Sprintf("|other=%d", other)
		variant := variants[signature]
		variant.Fields, variant.OtherFields, variant.Count = fields, other, variant.Count+1
		variants[signature] = variant
	}
	keys := make([]string, 0, len(variants))
	for key := range variants {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxVariants {
		keys, d.Truncated = keys[:maxVariants], true
	}
	for _, key := range keys {
		d.Shapes = append(d.Shapes, variants[key])
	}
	return d
}

func rejectAlliance(s *AllianceSnapshot, reason string) {
	if s.RejectionReasons == nil {
		s.RejectionReasons = map[string]int{}
	}
	s.RejectedRows++
	s.RejectionReasons[reason]++
}

func ParseAllianceSnapshot(s *AllianceSnapshot, q Query, p *sfs.SFSObject) error {
	if q.AllianceDetail {
		return parseAllianceDetailSnapshot(s, q, p)
	}
	if !q.AllianceRanking || q.Command != "alliance.rank" || q.Payload != "allianceRanking" || q.ValueField != "fightpower" {
		return errInvalidQuery
	}
	if p == nil {
		s.Status = "missing_payload"
		return nil
	}
	if selfRanking, present := p.Get("selfRanking"); present {
		s.SelfRankingType = sfsTypeName(selfRanking.Type)
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
			if !sfs.IsSensitiveSFSKey(key) && allianceResponseFields[key] {
				s.ResponseFields = append(s.ResponseFields, key)
			} else {
				s.ResponseOtherFields++
			}
		}
		sort.Strings(s.ResponseFields)
		s.Status = "missing_payload"
		return nil
	}
	var items []sfs.SFSValue
	rankByPosition := false
	switch collection := raw.Val.(type) {
	case *sfs.SFSArray:
		if collection == nil {
			s.Status = "partial"
			return errors.New("nil alliance ranking array")
		}
		items = collection.Items()
		rankByPosition = true
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
	s.RowShape = allianceRowShape(raw, items)
	s.PayloadPresent = true
	seen := map[string]bool{}
	for index, value := range items {
		row, ok := value.Val.(*sfs.SFSObject)
		if !ok || row == nil {
			rejectAlliance(s, "wrong_row_type")
			continue
		}
		allianceID := allianceIdentifier(row)
		power, powerOK := integer(row, "fightpower")
		if allianceID == "" {
			rejectAlliance(s, "missing_alliance_id")
			continue
		}
		if seen[allianceID] {
			rejectAlliance(s, "duplicate_alliance_id")
			continue
		}
		if !powerOK {
			rejectAlliance(s, "missing_or_wrong_power_type")
			continue
		}
		if power < 0 {
			rejectAlliance(s, "negative_power")
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
			rejectAlliance(s, "server_mismatch")
			continue
		}
		if rankByPosition {
			rank := int64(index + 1)
			record.Rank = &rank
		} else if rank, ok := integer(row, "rank"); ok && rank > 0 {
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
	// Shape metadata is an investigation aid for rejected or otherwise incomplete
	// payloads. A clean operational snapshot must remain inside the stable v2
	// ingestion contract rather than acquiring diagnostic-only top-level fields.
	if s.Status == "ok" {
		s.ResponseFields = nil
		s.ResponseOtherFields = 0
		s.SelfRankingType = ""
		s.RejectionReasons = nil
		s.RowShape = nil
	}
	return nil
}

func parseAllianceDetailSnapshot(s *AllianceSnapshot, q Query, p *sfs.SFSObject) error {
	if q.Command != "get.al.info" || q.EntityAllianceID == "" || q.Metric != "alliance_detail" {
		return errInvalidQuery
	}
	if p == nil {
		s.Status = "missing_payload"
		return nil
	}
	if code := p.GetString("errorCode"); code != "" {
		s.Status, s.ErrorCode = "server_error", code
		return nil
	}
	if v, ok := p.Get("success"); ok {
		if success, typed := v.Val.(bool); !typed || !success {
			s.Status = "server_error"
			return nil
		}
	}
	allianceID := allianceIdentifier(p)
	power, powerOK := integer(p, "fightPower")
	if !powerOK {
		power, powerOK = integer(p, "fightpower")
	}
	serverID, serverOK := integer(p, "ownerServerId")
	members, membersOK := integer(p, "curMember")
	if allianceID == "" || allianceID != q.EntityAllianceID || !powerOK || power < 0 ||
		!serverOK || serverID != int64(s.TargetServer) || !membersOK || members < 0 {
		s.Status = "partial"
		return errors.New("invalid alliance detail identity or metrics")
	}
	record := AllianceRecord{AllianceID: allianceID, Name: p.GetString("name"),
		Abbr: p.GetString("abbr"), Leader: p.GetString("leaderName"), ServerID: int32(serverID),
		TotalPower: power, MemberCount: members}
	s.Alliances = append(s.Alliances, record)
	s.PayloadPresent = true
	s.Status = "ok"
	s.Validation = "response_owner_server_match"
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
	if q.AllianceDetail {
		s.Parameters["allianceId"] = q.EntityAllianceID
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	s.CollectedAt = now().UTC()
	validRanking := q.AllianceRanking && q.Command == "alliance.rank" && c.SourceServer == c.TargetServer
	validDetail := q.AllianceDetail && q.Command == "get.al.info" && q.EntityAllianceID != ""
	if c.Client == nil || c.SourceServer <= 0 || c.AllianceID == "" || (!validRanking && !validDetail) || q.Period == "" {
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
	if q.AllianceRanking {
		s.Validation = "response_received_same_server"
	}
	err = ParseAllianceSnapshot(&s, q, message.Params)
	return s, err
}

func ExportAllianceJSONL(w io.Writer, s AllianceSnapshot) error {
	return json.NewEncoder(w).Encode(s)
}
