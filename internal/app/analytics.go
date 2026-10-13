package app

import (
	"context"
	"errors"
	"fmt"
	"lastwar-client/internal/analytics"
	"lastwar-client/internal/session"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// runAnalytics uses an existing authenticated socket; it performs no gameplay
// operations, retries, account creation or deployment. File output is private.
func runAnalytics(conn *session.GameConn, cfg *analytics.Config, sourceServer int32) error {
	queries, err := cfg.BuildQueries()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	d := conn.StartDispatcher()
	defer func() { _ = d.Close() }()
	collector := analytics.Collector{Client: d, SourceServer: sourceServer, TargetServer: cfg.TargetServer, AllianceID: cfg.AllianceID}
	var failures []error
	for len(queries) > 0 {
		q := queries[0]
		queries = queries[1:]
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if q.Metric == "meteorite_context" {
			snapshot, rounds, discoverErr := collector.DiscoverMeteorite(ctx, q)
			if exportErr := analytics.ExportJSONL(f, snapshot); exportErr != nil {
				return exportErr
			}
			if discoverErr != nil {
				failures = append(failures, fmt.Errorf("meteorite discovery: %w", discoverErr))
			}
			queries = append(rounds, queries...)
			slog.Info("meteorite context", "status", snapshot.Status, "stage", snapshot.Parameters["stage"], "grabTimes", snapshot.Parameters["grabTimes"], "roundQueries", len(rounds))
			continue
		}
		snapshot, err := collector.Collect(ctx, q)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", q.Metric, err))
		}
		if err := analytics.ExportJSONL(f, snapshot); err != nil {
			return err
		}
		if snapshot.Status != "ok" && snapshot.Status != "empty" && err == nil {
			failures = append(failures, fmt.Errorf("%s: %s", q.Metric, snapshot.Status))
		}
		slog.Info("analytics snapshot", "metric", q.Metric, "status", snapshot.Status, "rows", len(snapshot.Records), "rejected", snapshot.RejectedRows, "sourceServer", sourceServer)
		select {
		case <-d.Done():
			return errors.Join(append(failures, d.Err())...)
		default:
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return errors.Join(failures...)
}
