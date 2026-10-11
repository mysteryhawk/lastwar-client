package sfs

import (
	"encoding/json"
	"testing"
)

func TestJSONScalarRangeAndTypes(t *testing.T) {
	for _, tc := range []struct {
		kind, n string
		tag     byte
		valid   bool
	}{
		{"int", "2147483647", SFSInt, true}, {"int", "2147483648", 0, false}, {"long", "9223372036854775807", SFSLong, true}, {"long", "9223372036854775808", 0, false}, {"short", "-32768", sfsShort, true}, {"short", "-32769", 0, false}, {"byte", "255", sfsByte, true}, {"byte", "-1", 0, false}, {"int", "1.5", 0, false}, {"double", "1.5", SFSDouble, true}, {"double", "1e400", 0, false}, {"unknown", "1", 0, false},
	} {
		t.Run(tc.kind+tc.n, func(t *testing.T) {
			v, e := JSONScalar(map[string]any{"$sfs": tc.kind, "value": json.Number(tc.n)})
			if (e == nil) != tc.valid || (tc.valid && v.Type != tc.tag) {
				t.Fatalf("type %d error %v", v.Type, e)
			}
		})
	}
	for _, m := range []map[string]any{{"$sfs": "utf", "value": "text"}, {"$sfs": "bool", "value": true}} {
		if _, e := JSONScalar(m); e != nil {
			t.Fatal(e)
		}
	}
}
