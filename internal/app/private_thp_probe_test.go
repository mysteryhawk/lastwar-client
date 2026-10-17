package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lastwar-client/internal/session"
	"lastwar-client/internal/sfs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func privateTHPTestRow(uid string, power int64) *sfs.SFSObject {
	r := sfs.NewSFSObject()
	r.PutUtfString("uid", uid)
	r.PutUtfString("name", "Synthetic")
	r.PutLong("heroPower", power)
	r.PutLong("power", 900000)
	r.PutLong("armyPower", 800000)
	return r
}

func privateTHPTestPayload(rows ...*sfs.SFSObject) *sfs.SFSObject {
	p := sfs.NewSFSObject()
	users := sfs.NewSFSArray()
	for _, row := range rows {
		users.AddSFSObject(row)
	}
	p.PutSFSArray("users", users)
	p.PutUtfString("accessToken", "PRIVATE-SECRET-MUST-NOT-APPEAR")
	return p
}

// Respond to exactly one extension, then wait for the helper to close the socket.
// A second request is reported rather than silently drained.
func privateTHPTestServer(server *session.GameConn, payload *sfs.SFSObject) <-chan error {
	done := make(chan error, 1)
	go func() {
		msg, err := session.ReadNextExtension(server)
		if err != nil {
			done <- err
			return
		}
		if msg.Cmd != "dragon.assign.player.info" || len(session.ParamKeysWithoutID(msg.Params)) != 0 {
			done <- fmt.Errorf("unexpected query: %s", msg.Cmd)
			_ = server.Close()
			return
		}
		payload.PutInt("_id", msg.Params.GetInt("_id"))
		if err = server.SendExtension(msg.Cmd, payload); err != nil {
			done <- err
			return
		}
		if _, err = session.ReadNextExtension(server); !errors.Is(err, io.EOF) {
			done <- fmt.Errorf("expected close after one request: %v", err)
			return
		}
		done <- nil
	}()
	return done
}

func readPrivateTHPTestDocument(t *testing.T, path string) privateTHPDocument {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "PRIVATE-SECRET") || strings.Contains(string(b), "armyPower") || strings.Contains(string(b), "\"power\"") {
		t.Fatal("private export included unapproved response fields")
	}
	var document privateTHPDocument
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 9 {
		t.Fatalf("unexpected document fields: %v", raw)
	}
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(raw["records"], &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if len(record) != 3 || record["uid"] == nil || record["display_name"] == nil || record["thp"] == nil {
			t.Fatalf("unexpected record fields: %v", record)
		}
	}
	return document
}

func privateTHPTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPrivateTHPProbeHundredRows(t *testing.T) {
	c, s := session.NewPipeGameConnPair(t)
	rows := make([]*sfs.SFSObject, 100)
	for i := range rows {
		rows[i] = privateTHPTestRow(strconv.Itoa(i+1), int64(i))
	}
	rows[0] = privateTHPTestRow("9007199254740993", 9223372036854775807)
	done := privateTHPTestServer(s, privateTHPTestPayload(rows...))
	path := filepath.Join(privateTHPTestDir(t), "members.json")
	if err := runPrivateTHPProbe(c, 1723, path); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	document := readPrivateTHPTestDocument(t, path)
	if document.SchemaVersion != 1 || document.SourceServer != 1723 || document.Command != "dragon.assign.player.info" || document.Status != "complete" || document.ResponseStatus != "ok" || document.ScopeValidation != "manual_in_game_comparison_required" || document.CollectedAt.IsZero() || len(document.Records) != 100 {
		t.Fatalf("bad metadata: %+v", document)
	}
	if document.Records[0].UID != "9007199254740993" || *document.Records[0].THP != 9223372036854775807 || document.Records[0].DisplayName != "Synthetic" {
		t.Fatal("exact UID/power/name not preserved")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows mode bits do not model DACL privacy. The calling workflow uses a
	// private directory; POSIX output must be precisely owner-readable/writable.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestPrivateTHPProbeRetainsIncompleteAndRejectedReplies(t *testing.T) {
	missingNameRows := make([]*sfs.SFSObject, 100)
	for i := range missingNameRows {
		missingNameRows[i] = privateTHPTestRow(strconv.Itoa(i+1), int64(i+1))
	}
	missingNameRows[42].PutUtfString("name", "")
	for _, tc := range []struct {
		name            string
		rows            []*sfs.SFSObject
		status          string
		count, rejected int
	}{
		{"incomplete count", []*sfs.SFSObject{privateTHPTestRow("1", 0)}, "ok", 1, 0},
		{"missing display name", missingNameRows, "ok", 100, 0},
		{"duplicate UID", []*sfs.SFSObject{privateTHPTestRow("1", 100), privateTHPTestRow("1", 200)}, "partial", 1, 1},
		{"city and army are not THP", []*sfs.SFSObject{func() *sfs.SFSObject {
			r := sfs.NewSFSObject()
			r.PutUtfString("uid", "1")
			r.PutLong("power", 900000)
			r.PutLong("armyPower", 800000)
			return r
		}()}, "partial", 0, 1},
		{"empty", nil, "empty", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := session.NewPipeGameConnPair(t)
			done := privateTHPTestServer(s, privateTHPTestPayload(tc.rows...))
			path := filepath.Join(privateTHPTestDir(t), "members.json")
			if err := runPrivateTHPProbe(c, 1723, path); err == nil {
				t.Fatal("incomplete probe accepted")
			}
			_ = c.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			document := readPrivateTHPTestDocument(t, path)
			if document.Status != "incomplete" || document.ResponseStatus != tc.status || len(document.Records) != tc.count || document.RejectedRows != tc.rejected {
				t.Fatalf("unexpected retained reply: %+v", document)
			}
			if tc.name == "incomplete count" && *document.Records[0].THP != 0 {
				t.Fatal("explicit zero lost")
			}
		})
	}
}

func TestPrivateTHPProbeRefusesWrongServerAndExistingOutputBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		server   int32
		existing bool
	}{
		{"unknown server", 0, false}, {"wrong server", 1456, false}, {"existing file", 1723, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := session.NewPipeGameConnPair(t)
			path := filepath.Join(privateTHPTestDir(t), "members.json")
			if tc.existing {
				if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() {
				_, err := session.ReadNextExtension(s)
				done <- err
			}()
			if err := runPrivateTHPProbe(c, tc.server, path); err == nil {
				t.Fatal("invalid probe accepted")
			}
			_ = c.Close()
			if err := <-done; !errors.Is(err, io.EOF) {
				t.Fatalf("request sent before preflight: %v", err)
			}
			if tc.existing {
				b, err := os.ReadFile(path)
				if err != nil || string(b) != "original" {
					t.Fatalf("existing file changed: %q %v", b, err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("wrong-server probe created output: %v", err)
			}
		})
	}
}

func TestPrivateTHPProbeRefusesSymlink(t *testing.T) {
	dir := privateTHPTestDir(t)
	target := filepath.Join(dir, "target.json")
	path := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c, _ := session.NewPipeGameConnPair(t)
	if err := runPrivateTHPProbe(c, 1723, path); !os.IsExist(err) {
		t.Fatalf("symlink was not refused: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("symlink target created: %v", err)
	}
}
