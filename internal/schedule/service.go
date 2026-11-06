// Package schedule evaluates cron-based job templates. The API owns the
// scheduler loop; workers never read the schedules table directly.
package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/robfig/cron/v3"

	"relayforge/internal/database/db"
	"relayforge/internal/job"
	"relayforge/internal/project"
)

const (
	DefaultTimezone = "UTC"

	maxNameLength    = 100
	maxCronLength    = 200
	maxJobTypeLength = 100
	maxQueueName     = 100
	maxPayloadBytes  = 256 * 1024
	maxPriority      = 1000
	maxAttempts      = 100
	dueBatchSize     = 50
)

var (
	ErrInvalidName      = errors.New("schedule name must be between 1 and 100 characters")
	ErrInvalidCron      = errors.New("cron expression is invalid")
	ErrInvalidTimezone  = errors.New("timezone is invalid")
	ErrInvalidJobType   = errors.New("schedule job type must be between 1 and 100 characters")
	ErrInvalidQueueName = errors.New("schedule queue name must be at most 100 characters")
	ErrInvalidPayload   = errors.New("schedule payload must be a JSON object")
	ErrPayloadTooLarge  = errors.New("schedule payload must be at most 256 KiB")
	ErrInvalidPriority  = errors.New("schedule priority must be between 0 and 1000")
	ErrInvalidAttempts  = errors.New("schedule max attempts must be between 0 and 100")
	ErrScheduleNotFound = errors.New("schedule not found")
	ErrNameTaken        = errors.New("a schedule with that name already exists")
	ErrScheduleLimit    = errors.New("schedule limit reached for this project")
	ErrEmptyUpdate      = errors.New("update must contain at least one field")
	ErrInvalidProjectID = errors.New("invalid project id")
)

// Limits caps per-project schedules. A zero value means unlimited.
type Limits struct {
	MaxSchedulesPerProject int
}

type Service struct {
	queries  *db.Queries
	jobs     *job.Service
	projects *project.Service
	limits   Limits
}

func NewService(queries *db.Queries, jobs *job.Service, projects *project.Service, limits Limits) *Service {
	return &Service{queries: queries, jobs: jobs, projects: projects, limits: limits}
}

type CreateInput struct {
	Name        string
	Cron        string
	Timezone    string
	Enabled     bool
	JobType     string
	QueueName   string
	Payload     json.RawMessage
	Priority    int32
	MaxAttempts int32
}

type UpdateInput struct {
	Name        *string
	Cron        *string
	Timezone    *string
	Enabled     *bool
	JobType     *string
	QueueName   *string
	Payload     json.RawMessage
	Priority    *int32
	MaxAttempts *int32
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, input CreateInput) (db.Schedule, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.Schedule{}, err
	}
	if s.limits.MaxSchedulesPerProject > 0 {
		count, err := s.queries.CountSchedulesByProject(ctx, projectID)
		if err != nil {
			return db.Schedule{}, fmt.Errorf("count schedules: %w", err)
		}
		if count >= int64(s.limits.MaxSchedulesPerProject) {
			return db.Schedule{}, ErrScheduleLimit
		}
	}

	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = DefaultTimezone
	}
	name, err := validateName(input.Name)
	if err != nil {
		return db.Schedule{}, err
	}
	cronExpr, loc, err := validateCron(input.Cron, timezone)
	if err != nil {
		return db.Schedule{}, err
	}
	jobType, queueName, payload, err := validateTarget(input.JobType, input.QueueName, input.Payload)
	if err != nil {
		return db.Schedule{}, err
	}
	if err := validatePriorityAttempts(input.Priority, input.MaxAttempts); err != nil {
		return db.Schedule{}, err
	}

	next, err := nextRun(cronExpr, loc, time.Now())
	if err != nil {
		return db.Schedule{}, err
	}

	created, err := s.queries.CreateSchedule(ctx, db.CreateScheduleParams{
		ProjectID:   projectID,
		Name:        name,
		Cron:        strings.TrimSpace(input.Cron),
		Timezone:    timezone,
		Enabled:     input.Enabled,
		JobType:     jobType,
		QueueName:   queueName,
		Payload:     payload,
		Priority:    input.Priority,
		MaxAttempts: input.MaxAttempts,
		NextRunAt:   next,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Schedule{}, ErrNameTaken
		}
		return db.Schedule{}, fmt.Errorf("create schedule: %w", err)
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, projectID, scheduleID uuid.UUID) (db.Schedule, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.Schedule{}, err
	}
	found, err := s.queries.GetSchedule(ctx, db.GetScheduleParams{ID: scheduleID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Schedule{}, ErrScheduleNotFound
	}
	if err != nil {
		return db.Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	return found, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID) ([]db.Schedule, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}
	schedules, err := s.queries.ListSchedulesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return schedules, nil
}

func (s *Service) Update(ctx context.Context, userID, projectID, scheduleID uuid.UUID, input UpdateInput) (db.Schedule, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.Schedule{}, err
	}

	existing, err := s.queries.GetSchedule(ctx, db.GetScheduleParams{ID: scheduleID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Schedule{}, ErrScheduleNotFound
	}
	if err != nil {
		return db.Schedule{}, fmt.Errorf("get schedule: %w", err)
	}

	params := db.UpdateScheduleParams{ID: scheduleID, ProjectID: projectID}
	recomputeNext := false

	if input.Name != nil {
		name, err := validateName(*input.Name)
		if err != nil {
			return db.Schedule{}, err
		}
		params.Name = &name
	}
	if input.JobType != nil {
		jobType, _, _, err := validateTarget(*input.JobType, "", nil)
		if err != nil {
			return db.Schedule{}, err
		}
		params.JobType = &jobType
	}
	if input.QueueName != nil {
		queueName := strings.TrimSpace(*input.QueueName)
		if len(queueName) > maxQueueName {
			return db.Schedule{}, ErrInvalidQueueName
		}
		params.QueueName = &queueName
	}
	if len(input.Payload) > 0 {
		payload, err := normalizePayload(input.Payload)
		if err != nil {
			return db.Schedule{}, err
		}
		params.Payload = payload
	}
	if input.Priority != nil {
		if err := validatePriorityAttempts(*input.Priority, existing.MaxAttempts); err != nil {
			return db.Schedule{}, err
		}
		params.Priority = input.Priority
	}
	if input.MaxAttempts != nil {
		if err := validatePriorityAttempts(existing.Priority, *input.MaxAttempts); err != nil {
			return db.Schedule{}, err
		}
		params.MaxAttempts = input.MaxAttempts
	}
	if input.Enabled != nil {
		params.Enabled = input.Enabled
		recomputeNext = true
	}

	cronValue := existing.Cron
	timezone := existing.Timezone
	if input.Cron != nil {
		cronValue = *input.Cron
		recomputeNext = true
	}
	if input.Timezone != nil {
		timezone = strings.TrimSpace(*input.Timezone)
		if timezone == "" {
			timezone = DefaultTimezone
		}
		recomputeNext = true
	}
	if input.Cron != nil || input.Timezone != nil {
		if _, _, err := validateCron(cronValue, timezone); err != nil {
			return db.Schedule{}, err
		}
		params.Cron = &cronValue
		params.Timezone = &timezone
	}

	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if recomputeNext && enabled {
		cronExpr, loc, err := validateCron(cronValue, timezone)
		if err != nil {
			return db.Schedule{}, err
		}
		next, err := nextRun(cronExpr, loc, time.Now())
		if err != nil {
			return db.Schedule{}, err
		}
		params.NextRunAt = &next
	} else if recomputeNext && !enabled {
		// Paused schedules keep their next run; resume recomputes it.
		params.NextRunAt = nil
	}

	if !hasUpdate(params) {
		return db.Schedule{}, ErrEmptyUpdate
	}

	updated, err := s.queries.UpdateSchedule(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Schedule{}, ErrScheduleNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Schedule{}, ErrNameTaken
		}
		return db.Schedule{}, fmt.Errorf("update schedule: %w", err)
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, userID, projectID, scheduleID uuid.UUID) error {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return err
	}
	deleted, err := s.queries.DeleteSchedule(ctx, db.DeleteScheduleParams{ID: scheduleID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if deleted == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

// RunDue submits jobs for every schedule whose next run has elapsed. Submissions
// carry an idempotency key derived from the schedule and run time, and the
// schedule is advanced with an optimistic update, so multiple API instances can
// run this safely.
func (s *Service) RunDue(ctx context.Context) (int, error) {
	due, err := s.queries.ListDueSchedules(ctx, dueBatchSize)
	if err != nil {
		return 0, fmt.Errorf("list due schedules: %w", err)
	}

	submitted := 0
	for _, item := range due {
		if !item.Enabled {
			continue
		}

		result, err := s.jobs.Submit(ctx, job.SubmitInput{
			ProjectID:      item.ProjectID,
			QueueName:      item.QueueName,
			JobType:        item.JobType,
			Payload:        item.Payload,
			Priority:       optionalInt32(item.Priority),
			MaxAttempts:    optionalInt32(item.MaxAttempts),
			ScheduledAt:    time.Now(),
			IdempotencyKey: scheduleKey(item),
		})
		if err != nil {
			slog.Error("schedule submit failed",
				"schedule_id", item.ID,
				"project_id", item.ProjectID,
				"error", err)
			continue
		}

		cronExpr, loc, err := validateCron(item.Cron, item.Timezone)
		if err != nil {
			slog.Error("schedule has invalid cron; skipping advance",
				"schedule_id", item.ID, "error", err)
			continue
		}
		next, err := nextRun(cronExpr, loc, time.Now())
		if err != nil {
			slog.Error("compute next schedule run", "schedule_id", item.ID, "error", err)
			continue
		}
		advanced, err := s.queries.AdvanceSchedule(ctx, db.AdvanceScheduleParams{
			ID:          item.ID,
			NextRunAt:   next,
			NextRunAt_2: item.NextRunAt,
		})
		if err != nil {
			slog.Error("schedule advance failed", "schedule_id", item.ID, "error", err)
			continue
		}
		if advanced > 0 && !result.Replayed {
			submitted++
		}
	}
	return submitted, nil
}

func scheduleKey(item db.Schedule) string {
	return fmt.Sprintf("schedule:%s:%d", item.ID, item.NextRunAt.UTC().Unix())
}

func optionalInt32(value int32) *int32 {
	if value == 0 {
		return nil
	}
	return &value
}

func hasUpdate(params db.UpdateScheduleParams) bool {
	return params.Name != nil || params.Cron != nil || params.Timezone != nil ||
		params.Enabled != nil || params.JobType != nil || params.QueueName != nil ||
		params.Payload != nil || params.Priority != nil || params.MaxAttempts != nil ||
		params.NextRunAt != nil
}

func validateName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > maxNameLength {
		return "", ErrInvalidName
	}
	return name, nil
}

func validateCron(value, timezone string) (cron.Schedule, *time.Location, error) {
	expression := strings.TrimSpace(value)
	if expression == "" || len(expression) > maxCronLength {
		return nil, nil, ErrInvalidCron
	}
	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidCron, err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %q", ErrInvalidTimezone, timezone)
	}
	return parsed, loc, nil
}

func nextRun(expression cron.Schedule, loc *time.Location, after time.Time) (time.Time, error) {
	next := expression.Next(after.In(loc))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("%w: expression never fires", ErrInvalidCron)
	}
	return next.UTC(), nil
}

func validateTarget(jobType, queueName string, payload json.RawMessage) (string, string, json.RawMessage, error) {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" || len(jobType) > maxJobTypeLength {
		return "", "", nil, ErrInvalidJobType
	}
	queueName = strings.TrimSpace(queueName)
	if len(queueName) > maxQueueName {
		return "", "", nil, ErrInvalidQueueName
	}
	normalized, err := normalizePayload(payload)
	if err != nil {
		return "", "", nil, err
	}
	return jobType, queueName, normalized, nil
}

func normalizePayload(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > maxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	if !json.Valid(raw) {
		return nil, ErrInvalidPayload
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, ErrInvalidPayload
	}
	return raw, nil
}

func validatePriorityAttempts(priority, attempts int32) error {
	if priority < 0 || priority > maxPriority {
		return ErrInvalidPriority
	}
	if attempts < 0 || attempts > maxAttempts {
		return ErrInvalidAttempts
	}
	return nil
}
