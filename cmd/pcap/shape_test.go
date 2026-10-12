package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"lastwar-client/internal/sfs"
)

func TestShapeFromRequestDoesNotExposeCredentials(t *testing.T) {
	const (
		uid      = "sensitive-role-uid-1723"
		deviceID = "123e4567-e89b-12d3-a456-426614174000"
		token    = "sensitive-access-token"
		shumei   = "sensitive-shumei"
	)
	airKey := "lwDid_" + base64.StdEncoding.EncodeToString([]byte(deviceID))
	params := sfs.NewSFSObject()
	params.PutUtfString("gameUid", uid)
	params.PutUtfString("deviceId", deviceID)
	params.PutUtfString("at", token)
	params.PutUtfString("airKey", airKey)
	params.PutUtfString("shumeiBoxId", shumei)
	params.PutUtfString("packageName", "com.fun.lastwar.gp")
	params.PutUtfString("platform", "1")
	params.PutUtfString("pf", "market_global")
	params.PutUtfString("appVersion", "1.0.364")
	params.PutUtfString("versionCode", "1892")
	params.PutUtfString("serverId", "1723")
	params.PutUtfString("ta", `{"lw_zone":"APS1723","lw_device_id":"sensitive-nested-device"}`)
	req := sfs.NewSFSObject()
	req.PutUtfString("zn", "APS1723")
	req.PutUtfString("un", uid)
	req.PutSFSObject("p", params)

	report := shapeFromRequest(4, req, params)
	var out bytes.Buffer
	if err := json.NewEncoder(&out).Encode(report); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, secret := range []string{uid, deviceID, token, airKey, shumei, "sensitive-nested-device"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential-free report leaked %q: %s", secret, got)
		}
	}
	if !report.UIDFieldsPresent || report.UIDFieldsEqual == nil || !*report.UIDFieldsEqual || report.UIDLength != len(uid) || report.DeviceIDLength != len(deviceID) {
		t.Fatalf("relationship/length summary incorrect: %+v", report)
	}
	if report.AirKeyDerived == nil || !*report.AirKeyDerived || report.DeviceIDShape != "uuid" || report.DeviceIDSuffix != "none" {
		t.Fatalf("device relationship/shape summary incorrect: %+v", report)
	}
	if report.TAZoneMatchesLogin == nil || !*report.TAZoneMatchesLogin {
		t.Fatalf("ta zone relationship missing or false: %+v", report)
	}
}

func TestDeviceIDShapeRecognisesGameSuffixes(t *testing.T) {
	for _, tc := range []struct {
		id, shape, suffix string
	}{
		{"0123456789abcdef0123456789abcdef_n3d", "hex32", "n3d"},
		{"123e4567-e89b-12d3-a456-426614174000_3d", "uuid", "3d"},
		{"not-a-device_n3d", "opaque", "n3d"},
	} {
		if got := deviceIDShape(tc.id); got != tc.shape {
			t.Errorf("deviceIDShape(%q) = %q, want %q", tc.id, got, tc.shape)
		}
		if got := deviceIDSuffix(tc.id); got != tc.suffix {
			t.Errorf("deviceIDSuffix(%q) = %q, want %q", tc.id, got, tc.suffix)
		}
	}
}

func TestShapeFromRequestDoesNotCompareNullTAZone(t *testing.T) {
	params := sfs.NewSFSObject()
	params.PutUtfString("ta", `{"lw_zone":null}`)
	req := sfs.NewSFSObject()
	req.PutUtfString("zn", "APS1723")
	report := shapeFromRequest(0, req, params)
	if report.TAZoneMatchesLogin != nil {
		t.Fatalf("null TA zone reported as comparable: %+v", report)
	}
}

func TestShapeFromRequestDoesNotEquateMissingUIDs(t *testing.T) {
	params := sfs.NewSFSObject()
	req := sfs.NewSFSObject()
	report := shapeFromRequest(0, req, params)
	if report.UIDFieldsPresent || report.UIDFieldsEqual != nil {
		t.Fatalf("missing UIDs reported as comparable: %+v", report)
	}
}

func TestSafeRejectionLabelAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		want   string
	}{
		{"e005", []string{"E005"}, "E005"},
		{"e011", []string{"E011"}, "E011"},
		{"credential marker", []string{"token-secret"}, ""},
		{"multiple", []string{"E005", "E011"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := sfs.NewSFSObject()
			resp.PutValue("ep", sfs.SFSValue{Type: 16, Val: tc.labels})
			if got := safeRejectionLabel(resp); got != tc.want {
				t.Fatalf("safeRejectionLabel() = %q, want %q", got, tc.want)
			}
		})
	}
	wrongType := sfs.NewSFSObject()
	wrongType.PutUtfString("ep", "E005")
	if got := safeRejectionLabel(wrongType); got != "" {
		t.Fatalf("wrong-typed label escaped allowlist: %q", got)
	}
}

func TestSummarizeLoginShapeMarksMultipleRequestsAmbiguous(t *testing.T) {
	requestPacket := func(uid string) []byte {
		params := sfs.NewSFSObject()
		params.PutUtfString("gameUid", uid)
		req := sfs.NewSFSObject()
		req.PutUtfString("un", uid)
		req.PutUtfString("zn", "APS1723")
		req.PutSFSObject("p", params)
		outer := sfs.NewSFSObject()
		outer.PutByte("c", 0)
		outer.PutShort("a", 1)
		outer.PutSFSObject("p", req)
		return encodedPacket(t, outer)
	}
	respParams := sfs.NewSFSObject()
	resp := sfs.NewSFSObject()
	resp.PutByte("c", 0)
	resp.PutShort("a", 1)
	resp.PutSFSObject("p", respParams)

	c2s := append(requestPacket("first"), requestPacket("second")...)
	report := summarizeLoginShape(2, c2s, encodedPacket(t, resp))
	if report == nil || report.Response != "ambiguous_multiple_logins" || report.RequestCount != 2 || report.ResponseCount != 1 {
		t.Fatalf("multiple requests were incorrectly paired: %+v", report)
	}
	if len(report.EnvelopeFields) != 3 || report.EnvelopeFields[0].Type != "byte" || report.EnvelopeFields[1].Type != "short" {
		t.Fatalf("outer envelope types missing or wrong: %+v", report.EnvelopeFields)
	}
}

func encodedPacket(t *testing.T, obj *sfs.SFSObject) []byte {
	t.Helper()
	body, err := sfs.EncodeObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := sfs.EncodePacket(body)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestSFSFieldTypeNames(t *testing.T) {
	for tag, want := range map[byte]string{0: "null", 1: "bool", 4: "int", 5: "long", 8: "utf_string", 18: "object", 20: "text", 255: "unknown_255"} {
		if got := sfsTypeName(tag); got != want {
			t.Errorf("sfsTypeName(%d) = %q, want %q", tag, got, want)
		}
	}
}
