package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"

	"lastwar-client/internal/pcap"
	"lastwar-client/internal/sfs"
)

// loginShapeReport deliberately contains no credential or stable identifier values.
// It is safe to share for protocol comparison; lengths and equality relationships are
// useful for diagnosing the fresh-Android role-login mismatch without exporting a token,
// UID, device ID, air key, ShuMei value, email, or opaque analytics payload.
type loginShapeReport struct {
	Stream             int          `json:"stream"`
	RequestCount       int          `json:"request_count"`
	ResponseCount      int          `json:"response_count"`
	Response           string       `json:"response"`
	RejectionCode      int64        `json:"rejection_code,omitempty"`
	RejectionLabel     string       `json:"rejection_label,omitempty"`
	Zone               string       `json:"zone"`
	UIDLength          int          `json:"uid_length"`
	UIDFieldsPresent   bool         `json:"uid_fields_present"`
	UIDFieldsEqual     *bool        `json:"uid_fields_equal,omitempty"`
	DeviceIDLength     int          `json:"device_id_length"`
	DeviceIDShape      string       `json:"device_id_shape"`
	DeviceIDSuffix     string       `json:"device_id_suffix"`
	AirKeyLength       int          `json:"air_key_length"`
	AirKeyDerived      *bool        `json:"air_key_derived,omitempty"`
	AccessTokenLength  int          `json:"access_token_length"`
	ShumeiLength       int          `json:"shumei_length"`
	PackageName        string       `json:"package_name"`
	Platform           string       `json:"platform"`
	Storefront         string       `json:"storefront"`
	AppVersion         string       `json:"app_version"`
	VersionCode        string       `json:"version_code"`
	ServerID           string       `json:"server_id"`
	ResVersion         string       `json:"res_version"`
	DataConfigPresent  bool         `json:"data_config_present"`
	DataConfigLength   int          `json:"data_config_length"`
	TAValidJSONObject  bool         `json:"ta_valid_json_object"`
	TAKeys             []string     `json:"ta_keys"`
	TAFields           []shapeField `json:"ta_fields"`
	TAZoneMatchesLogin *bool        `json:"ta_zone_matches_login,omitempty"`
	EnvelopeFields     []shapeField `json:"envelope_fields"`
	LoginFields        []shapeField `json:"login_fields"`
	Fields             []shapeField `json:"fields"`
}

type shapeField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Empty *bool  `json:"empty,omitempty"`
}

func runLoginShape(in string, stream int, clientStr string, out io.Writer) error {
	if in == "" {
		return errors.New("-in is required")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	pkts, err := pcap.Parse(data)
	if err != nil {
		return err
	}
	convs := pcap.Conversations(pkts)
	if len(convs) == 0 {
		return fmt.Errorf("no TCP conversations with payload found in %s", in)
	}
	var explicit netip.Addr
	if clientStr != "" {
		explicit, err = netip.ParseAddr(clientStr)
		if err != nil {
			return fmt.Errorf("bad -client %q: %w", clientStr, err)
		}
	}
	candidates := []int{stream}
	if stream < 0 {
		candidates = candidates[:0]
		for i, conv := range convs {
			if !conv.TLS {
				candidates = append(candidates, i)
			}
		}
	} else if stream >= len(convs) {
		return fmt.Errorf("choose a -stream between 0 and %d", len(convs)-1)
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	found := 0
	for _, i := range candidates {
		client, err := convs[i].Client(explicit)
		if err != nil {
			if stream >= 0 {
				return err
			}
			continue
		}
		c2s, s2c := convs[i].Reassemble(client)
		report := summarizeLoginShape(i, c2s, s2c)
		if report == nil {
			continue
		}
		found++
		if err := enc.Encode(report); err != nil {
			return err
		}
	}
	if found == 0 {
		return errors.New("no Login request found in selected plain stream(s)")
	}
	return nil
}

func summarizeLoginShape(stream int, c2s, s2c []byte) *loginShapeReport {
	var requests []*sfs.SFSObject
	var envelopes []*sfs.SFSObject
	for _, obj := range decodeAll(c2s) {
		if isLogin(obj) {
			if req := sfsObject(obj, "p"); req != nil {
				requests = append(requests, req)
				envelopes = append(envelopes, obj)
			}
		}
	}
	if len(requests) == 0 {
		return nil
	}
	req := requests[len(requests)-1]
	params := sfsObject(req, "p")
	if params == nil {
		return nil
	}
	report := shapeFromRequest(stream, req, params)
	for _, key := range envelopes[len(envelopes)-1].Keys() {
		value, _ := envelopes[len(envelopes)-1].Get(key)
		report.EnvelopeFields = append(report.EnvelopeFields, shapeField{Name: key, Type: sfsTypeName(value.Type)})
	}
	report.RequestCount = len(requests)
	report.Response = "missing"
	var responses []*sfs.SFSObject
	for _, obj := range decodeAll(s2c) {
		if !isLogin(obj) {
			continue
		}
		resp := sfsObject(obj, "p")
		if resp == nil {
			continue
		}
		responses = append(responses, resp)
	}
	report.ResponseCount = len(responses)
	if len(requests) != 1 || len(responses) > 1 {
		report.Response = "ambiguous_multiple_logins"
		return report
	}
	if len(responses) == 1 {
		resp := responses[0]
		if resp.Has("ec") {
			report.Response = "rejected"
			report.RejectionCode = resp.GetLong("ec")
			report.RejectionLabel = safeRejectionLabel(resp)
		} else {
			report.Response = "accepted"
		}
	}
	return report
}

func shapeFromRequest(stream int, req, params *sfs.SFSObject) *loginShapeReport {
	uid, uidOK := stringField(params, "gameUid")
	username, usernameOK := stringField(req, "un")
	zone, _ := stringField(req, "zn")
	deviceID, deviceOK := stringField(params, "deviceId")
	airKey, airOK := stringField(params, "airKey")
	r := &loginShapeReport{
		Stream:            stream,
		Zone:              zone,
		UIDLength:         len(uid),
		UIDFieldsPresent:  uidOK && usernameOK,
		DeviceIDLength:    len(deviceID),
		DeviceIDShape:     deviceIDShape(deviceID),
		DeviceIDSuffix:    deviceIDSuffix(deviceID),
		AirKeyLength:      len(airKey),
		AccessTokenLength: len(params.GetString("at")),
		ShumeiLength:      len(params.GetString("shumeiBoxId")),
		PackageName:       params.GetString("packageName"),
		Platform:          params.GetString("platform"),
		Storefront:        params.GetString("pf"),
		AppVersion:        params.GetString("appVersion"),
		VersionCode:       params.GetString("versionCode"),
		ServerID:          params.GetString("serverId"),
		ResVersion:        params.GetString("resVersion"),
		DataConfigPresent: params.Has("dataConfigMd5"),
		DataConfigLength:  len(params.GetString("dataConfigMd5")),
	}
	if r.UIDFieldsPresent {
		equal := uid == username
		r.UIDFieldsEqual = &equal
	}
	if deviceOK && airOK {
		derived := airKey == "lwDid_"+base64.StdEncoding.EncodeToString([]byte(deviceID))
		r.AirKeyDerived = &derived
	}
	for _, key := range req.Keys() {
		value, _ := req.Get(key)
		r.LoginFields = append(r.LoginFields, describeShapeField(key, value))
	}
	for _, key := range params.Keys() {
		value, _ := params.Get(key)
		r.Fields = append(r.Fields, describeShapeField(key, value))
	}
	if raw := params.GetString("ta"); raw != "" {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &obj) == nil && obj != nil {
			r.TAValidJSONObject = true
			for key := range obj {
				r.TAKeys = append(r.TAKeys, key)
				r.TAFields = append(r.TAFields, shapeField{Name: key, Type: jsonType(obj[key])})
			}
			sort.Strings(r.TAKeys)
			sort.Slice(r.TAFields, func(i, j int) bool { return r.TAFields[i].Name < r.TAFields[j].Name })
			if zoneRaw, ok := obj["lw_zone"]; ok {
				var zone string
				if r.Zone != "" && jsonType(zoneRaw) == "string" && json.Unmarshal(zoneRaw, &zone) == nil {
					matches := zone == r.Zone
					r.TAZoneMatchesLogin = &matches
				}
			}
		}
	}
	return r
}

func describeShapeField(name string, value sfs.SFSValue) shapeField {
	f := shapeField{Name: name, Type: sfsTypeName(value.Type)}
	if value.Type == sfs.SFSUtfString || value.Type == sfs.SFSText {
		if s, ok := value.Val.(string); ok {
			empty := s == ""
			f.Empty = &empty
		}
	}
	return f
}

func stringField(obj *sfs.SFSObject, key string) (string, bool) {
	v, ok := obj.Get(key)
	if !ok || (v.Type != sfs.SFSUtfString && v.Type != sfs.SFSText) {
		return "", false
	}
	s, ok := v.Val.(string)
	return s, ok
}

func safeRejectionLabel(resp *sfs.SFSObject) string {
	v, ok := resp.Get("ep")
	if !ok || v.Type != 16 {
		return ""
	}
	labels, ok := v.Val.([]string)
	if !ok || len(labels) != 1 {
		return ""
	}
	s := labels[0]
	if len(s) != 4 || s[0] != 'E' || s[1] < '0' || s[1] > '9' || s[2] < '0' || s[2] > '9' || s[3] < '0' || s[3] > '9' {
		return ""
	}
	return s
}

func deviceIDShape(s string) string {
	if s == "" {
		return "missing"
	}
	base := strings.TrimSuffix(strings.TrimSuffix(s, "_n3d"), "_3d")
	if len(base) == 32 && isHex(base) {
		return "hex32"
	}
	parts := strings.Split(base, "-")
	if len(parts) == 5 && len(parts[0]) == 8 && len(parts[1]) == 4 && len(parts[2]) == 4 && len(parts[3]) == 4 && len(parts[4]) == 12 {
		for _, p := range parts {
			if !isHex(p) {
				return "opaque"
			}
		}
		return "uuid"
	}
	return "opaque"
}

func deviceIDSuffix(s string) string {
	if strings.HasSuffix(s, "_n3d") {
		return "n3d"
	}
	if strings.HasSuffix(s, "_3d") {
		return "3d"
	}
	if s == "" {
		return "missing"
	}
	return "none"
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func jsonType(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "invalid"
	}
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

func sfsTypeName(t byte) string {
	names := [...]string{"null", "bool", "byte", "short", "int", "long", "float", "double", "utf_string", "bool_array", "byte_array", "short_array", "int_array", "long_array", "float_array", "double_array", "utf_string_array", "array", "object", "class", "text"}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("unknown_%d", t)
}
