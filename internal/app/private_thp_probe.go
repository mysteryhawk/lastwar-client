package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"lastwar-client/internal/analytics"
	"lastwar-client/internal/session"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type privateTHPRecord struct {
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
	THP         *int64 `json:"thp"`
}

type privateTHPDocument struct {
	SchemaVersion   int                `json:"schema_version"`
	CollectedAt     time.Time          `json:"collected_at"`
	SourceServer    int32              `json:"source_server"`
	Command         string             `json:"command"`
	Status          string             `json:"status"` // complete or incomplete coverage validation
	ResponseStatus  string             `json:"response_status"`
	ScopeValidation string             `json:"scope_validation"`
	RejectedRows    int                `json:"rejected_rows"`
	Records         []privateTHPRecord `json:"records"`
}

// runPrivateTHPProbe makes one own-alliance Desert Storm THP request on an
// already authenticated socket. The caller must establish the observed role's
// server and alliance membership; this helper cannot infer either from email
// verification. Received normalized data is retained even when coverage fails.
// The caller supplies a new output path in a private directory outside the repo.
func runPrivateTHPProbe(conn *session.GameConn, sourceServer int32, outputPath string) (retErr error) {
	if sourceServer != 1723 {
		return errors.New("private THP probe requires observed source server 1723")
	}
	if conn == nil {
		return errors.New("private THP probe requires an authenticated connection")
	}
	if !filepath.IsAbs(outputPath) {
		return errors.New("private THP probe output path must be absolute")
	}
	parentInfo, err := os.Stat(filepath.Dir(outputPath))
	if err != nil {
		return fmt.Errorf("private THP probe output directory: %w", err)
	}
	if !parentInfo.IsDir() {
		return errors.New("private THP probe output parent is not a directory")
	}
	if runtime.GOOS != "windows" && parentInfo.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("private THP probe output directory must not be accessible by group/other: mode=%04o", parentInfo.Mode().Perm())
	}
	// O_EXCL rejects existing files, directories and symlinks, including dangling
	// symlinks, without following or modifying them. Never append private data.
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	if err := f.Chmod(0600); err != nil {
		return err
	}

	d := conn.StartDispatcher()
	defer func() { _ = d.Close() }()
	collector := analytics.Collector{
		Client: d, SourceServer: sourceServer, TargetServer: sourceServer,
		// This label is internal collection context only. The command has no
		// alliance parameter and the private document makes no alliance claim.
		AllianceID: "own-alliance",
	}
	snapshot, collectErr := collector.Collect(context.Background(), analytics.DesertTHPQuery("one-shot"))
	document := privateTHPDocument{
		SchemaVersion: 1, CollectedAt: snapshot.CollectedAt,
		SourceServer: snapshot.SourceServer, Command: snapshot.Command,
		ResponseStatus: snapshot.Status, RejectedRows: snapshot.RejectedRows,
		ScopeValidation: "manual_in_game_comparison_required",
		Records:         make([]privateTHPRecord, 0, len(snapshot.Records)),
	}
	seen := make(map[string]bool, len(snapshot.Records))
	valid := true
	for _, record := range snapshot.Records {
		if record.UID == "" || record.UID == "0" || record.Name == "" || seen[record.UID] || record.Value == nil || *record.Value < 0 {
			valid = false
		}
		seen[record.UID] = true
		document.Records = append(document.Records, privateTHPRecord{
			UID: record.UID, DisplayName: record.Name, THP: record.Value,
		})
	}
	document.Status = "incomplete"
	if snapshot.Status == "ok" && len(document.Records) == 100 && valid {
		document.Status = "complete"
	}
	if err := json.NewEncoder(f).Encode(document); err != nil {
		return errors.Join(collectErr, err)
	}
	if err := f.Sync(); err != nil {
		return errors.Join(collectErr, err)
	}
	slog.Info("private THP probe", "status", document.Status, "responseStatus", snapshot.Status, "rows", len(document.Records), "rejected", snapshot.RejectedRows)
	if document.Status != "complete" {
		return errors.Join(collectErr, fmt.Errorf("private THP probe requires 100 valid unique records: status=%s rows=%d rejected=%d", snapshot.Status, len(document.Records), snapshot.RejectedRows))
	}
	return collectErr
}
