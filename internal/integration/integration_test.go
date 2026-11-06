package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"relayforge/internal/auth"
	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/invite"
	"relayforge/internal/job"
	"relayforge/internal/outbox"
	"relayforge/internal/project"
	"relayforge/internal/schedule"
	"relayforge/internal/workerpool"
	"relayforge/internal/workertoken"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func uniqueEmail(prefix string) string {
	return prefix + "+" + uuid.NewString() + "@example.com"
}

func registerUser(t *testing.T, pool *pgxpool.Pool, mode string) (db.User, string) {
	t.Helper()
	queries := db.New(pool)
	service := auth.NewService(queries, time.Hour, auth.Options{Pool: pool, RegistrationMode: mode})
	email := uniqueEmail("integration")
	user, err := service.Register(context.Background(), email, "password123", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user, email
}

func TestInviteRegistrationIsSingleUse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	queries := db.New(pool)

	invites := invite.NewService(queries, time.Hour)
	created, err := invites.Create(ctx, invite.CreateInput{TTL: time.Hour})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	t.Cleanup(func() { _ = invites.Revoke(ctx, created.Invite.ID) })

	service := auth.NewService(queries, time.Hour, auth.Options{
		Pool:             pool,
		RegistrationMode: auth.RegistrationInvite,
	})
	email := uniqueEmail("invite")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email)
	})

	if _, err := service.Register(ctx, email, "password123", ""); !errors.Is(err, auth.ErrInviteRequired) {
		t.Fatalf("register without invite error = %v, want ErrInviteRequired", err)
	}
	user, err := service.Register(ctx, email, "password123", created.Token)
	if err != nil {
		t.Fatalf("register with invite: %v", err)
	}
	if user.Role != invite.RoleUser {
		t.Fatalf("role = %q, want user", user.Role)
	}
	if _, err := service.Register(ctx, uniqueEmail("reuse"), "password123", created.Token); !errors.Is(err, auth.ErrInviteInvalid) {
		t.Fatalf("reused invite error = %v, want ErrInviteInvalid", err)
	}
}

func TestProjectAndPoolQuotas(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	user, _ := registerUser(t, pool, auth.RegistrationOpen)

	projects := project.NewService(queries, project.Limits{MaxProjectsPerUser: 1})
	created, err := projects.Create(ctx, user.ID, "one")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := projects.Create(ctx, user.ID, "two"); !errors.Is(err, project.ErrProjectLimit) {
		t.Fatalf("second project error = %v, want ErrProjectLimit", err)
	}

	tokens := workertoken.NewService(queries, projects, "integration-pepper-value")
	pools := workerpool.NewService(queries, projects, tokens, workerpool.Limits{
		MaxPoolsPerProject:       1,
		MaxActivePoolsPerProject: 1,
		MaxConcurrencyPerProject: 4,
	})
	first, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "pool", Concurrency: 4})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if _, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "pool-2"}); !errors.Is(err, workerpool.ErrPoolLimit) {
		t.Fatalf("second pool error = %v, want ErrPoolLimit", err)
	}
	concurrency := int32(5)
	if _, err := pools.Update(ctx, user.ID, created.ID, first.ID, workerpool.UpdateInput{Concurrency: &concurrency}); !errors.Is(err, workerpool.ErrConcurrencyLimit) {
		t.Fatalf("raise concurrency error = %v, want ErrConcurrencyLimit", err)
	}
	paused := workerpool.StatusPaused
	if _, err := pools.Update(ctx, user.ID, created.ID, first.ID, workerpool.UpdateInput{Status: &paused}); err != nil {
		t.Fatalf("pause pool: %v", err)
	}
	if _, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "pool-3"}); !errors.Is(err, workerpool.ErrPoolLimit) {
		t.Fatalf("pool limit after pause error = %v, want ErrPoolLimit", err)
	}
}

func TestJobLifecycleAndLeaseRetry(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	user, _ := registerUser(t, pool, auth.RegistrationOpen)

	projects := project.NewService(queries, project.Limits{MaxProjectsPerUser: 5})
	created, err := projects.Create(ctx, user.ID, "jobs")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	jobs := job.NewService(pool, job.Limits{MaxQueuedJobsPerProject: 1})
	submitted, err := jobs.Submit(ctx, job.SubmitInput{
		ProjectID:   created.ID,
		JobType:     "noop",
		Payload:     []byte(`{}`),
		ScheduledAt: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("submit job: %v", err)
	}
	if _, err := jobs.Submit(ctx, job.SubmitInput{
		ProjectID: created.ID,
		JobType:   "noop",
		Payload:   []byte(`{}`),
	}); !errors.Is(err, job.ErrQueueLimit) {
		t.Fatalf("second submit error = %v, want ErrQueueLimit", err)
	}

	dispatcher := outbox.NewDispatcher(pool, outbox.PostgresNotifier{})
	if _, err := dispatcher.ProcessBatch(ctx); err != nil {
		t.Fatalf("dispatch outbox: %v", err)
	}

	worker, err := queries.CreateWorker(ctx, db.CreateWorkerParams{
		ProjectID: created.ID,
		Name:      "integration-worker",
		Status:    "online",
		Mode:      "remote",
	})
	if err != nil {
		t.Fatalf("create worker: %v", err)
	}

	executionService := execution.NewService(pool, execution.RetryPolicy{
		Backoff: execution.BackoffFixed,
		Base:    time.Second,
		Max:     time.Second,
	}, nil, 0, 0)

	claim, err := executionService.ClaimJob(ctx, execution.ClaimParams{
		ProjectID: created.ID,
		WorkerID:  worker.ID,
		Lease:     100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.Job.ID != submitted.Job.ID {
		t.Fatalf("claimed job = %s, want %s", claim.Job.ID, submitted.Job.ID)
	}

	time.Sleep(250 * time.Millisecond)
	reaped, err := executionService.ReapExpired(ctx, 10, func(db.Job, db.Execution) execution.RetryPolicy {
		return execution.RetryPolicy{Backoff: execution.BackoffFixed, Base: time.Second, Max: time.Second}
	})
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if reaped == 0 {
		t.Fatal("expected one expired lease to be reaped")
	}

	stored, err := queries.GetJob(ctx, submitted.Job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status != "scheduled" || stored.Attempts != 1 {
		t.Fatalf("job status/attempts = %s/%d, want scheduled/1", stored.Status, stored.Attempts)
	}
	executions, err := queries.ListExecutionsByJob(ctx, submitted.Job.ID)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if len(executions) != 1 || executions[0].ErrorMessage == nil || *executions[0].ErrorMessage == "" {
		t.Fatalf("execution error message not recorded: %+v", executions)
	}

	if _, err := dispatcher.ProcessBatch(ctx); err != nil {
		t.Fatalf("dispatch retry outbox: %v", err)
	}
	time.Sleep(1600 * time.Millisecond)
	retryClaim, err := executionService.ClaimJob(ctx, execution.ClaimParams{
		ProjectID: created.ID,
		WorkerID:  worker.ID,
		Lease:     time.Minute,
	})
	if err != nil {
		t.Fatalf("retry claim: %v", err)
	}
	if retryClaim.Job.Attempts != 2 {
		t.Fatalf("retry attempts = %d, want 2", retryClaim.Job.Attempts)
	}
	if err := executionService.SucceedExecution(ctx, created.ID, worker.ID, retryClaim.Execution.ID, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	final, err := queries.GetJob(ctx, submitted.Job.ID)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	if final.Status != "succeeded" {
		t.Fatalf("final status = %s, want succeeded", final.Status)
	}
}

func TestActivePoolQuotaFreesWhenPaused(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	user, _ := registerUser(t, pool, auth.RegistrationOpen)

	projects := project.NewService(queries, project.Limits{MaxProjectsPerUser: 5})
	created, err := projects.Create(ctx, user.ID, "active-quota")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	tokens := workertoken.NewService(queries, projects, "integration-pepper-value")
	pools := workerpool.NewService(queries, projects, tokens, workerpool.Limits{
		MaxPoolsPerProject:       3,
		MaxActivePoolsPerProject: 1,
		MaxConcurrencyPerProject: 10,
	})

	first, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "first", Concurrency: 1})
	if err != nil {
		t.Fatalf("create first pool: %v", err)
	}
	if _, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "second"}); !errors.Is(err, workerpool.ErrActivePoolLimit) {
		t.Fatalf("second active pool error = %v, want ErrActivePoolLimit", err)
	}

	paused := workerpool.StatusPaused
	if _, err := pools.Update(ctx, user.ID, created.ID, first.ID, workerpool.UpdateInput{Status: &paused}); err != nil {
		t.Fatalf("pause first pool: %v", err)
	}
	if _, err := pools.Create(ctx, user.ID, created.ID, workerpool.CreateInput{Name: "second"}); err != nil {
		t.Fatalf("create second pool after pause: %v", err)
	}

	active := workerpool.StatusActive
	if _, err := pools.Update(ctx, user.ID, created.ID, first.ID, workerpool.UpdateInput{Status: &active}); !errors.Is(err, workerpool.ErrActivePoolLimit) {
		t.Fatalf("resume first pool error = %v, want ErrActivePoolLimit", err)
	}
}

func TestScheduleRunDueSubmitsOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	user, _ := registerUser(t, pool, auth.RegistrationOpen)

	projects := project.NewService(queries, project.Limits{MaxProjectsPerUser: 5})
	created, err := projects.Create(ctx, user.ID, "schedules")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	jobs := job.NewService(pool, job.Limits{MaxQueuedJobsPerProject: 100})
	schedules := schedule.NewService(queries, jobs, projects, schedule.Limits{MaxSchedulesPerProject: 5})

	item, err := schedules.Create(ctx, user.ID, created.ID, schedule.CreateInput{
		Name:     "every-minute",
		Cron:     "* * * * *",
		Timezone: "UTC",
		Enabled:  true,
		JobType:  "noop",
		Payload:  []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM schedules WHERE id = $1", item.ID)
	})

	if _, err := pool.Exec(ctx, "UPDATE schedules SET next_run_at = now() - interval '1 second' WHERE id = $1", item.ID); err != nil {
		t.Fatalf("make schedule due: %v", err)
	}

	submitted, err := schedules.RunDue(ctx)
	if err != nil {
		t.Fatalf("run due: %v", err)
	}
	if submitted != 1 {
		t.Fatalf("submitted = %d, want 1", submitted)
	}

	updated, err := queries.GetSchedule(ctx, db.GetScheduleParams{ID: item.ID, ProjectID: created.ID})
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	if !updated.NextRunAt.After(time.Now()) {
		t.Fatalf("next run was not advanced: %s", updated.NextRunAt)
	}
	if updated.LastRunAt == nil {
		t.Fatal("last run was not recorded")
	}

	again, err := schedules.RunDue(ctx)
	if err != nil {
		t.Fatalf("second run due: %v", err)
	}
	if again != 0 {
		t.Fatalf("second run submitted = %d, want 0", again)
	}

	paused := false
	if _, err := schedules.Update(ctx, user.ID, created.ID, item.ID, schedule.UpdateInput{Enabled: &paused}); err != nil {
		t.Fatalf("pause schedule: %v", err)
	}
	list, err := jobs.List(ctx, created.ID, 50, 0)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	found := false
	for _, jobRecord := range list {
		if jobRecord.JobType == "noop" {
			found = true
		}
	}
	if !found {
		t.Fatal("schedule did not create a job")
	}
}
