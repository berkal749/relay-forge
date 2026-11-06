package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

// RunCleanup periodically deletes terminal jobs that exceed the project's
// retention window. A retention of zero disables cleanup.
func RunCleanup(ctx context.Context, queries *db.Queries, projectID uuid.UUID, interval time.Duration) {
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	cleanup := func() {
		project, err := queries.GetProject(ctx, projectID)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("load project retention settings", "error", err)
			}
			return
		}
		if project.JobRetentionDays <= 0 {
			return
		}

		deleted, err := queries.DeleteExpiredJobs(ctx, db.DeleteExpiredJobsParams{
			ProjectID:     projectID,
			RetentionDays: project.JobRetentionDays,
		})
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("delete expired jobs", "error", err)
			}
			return
		}
		if deleted > 0 {
			slog.Info("deleted expired jobs", "count", deleted, "retention_days", project.JobRetentionDays)
		}
	}

	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
