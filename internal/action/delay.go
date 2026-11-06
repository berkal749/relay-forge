package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/job"
)

const (
	maxDelaySeconds  = 30 * 24 * 60 * 60
	maxScheduleAhead = 366 * 24 * time.Hour
)

// SubmitJobInput is the subset of job submission a handler may use.
type SubmitJobInput struct {
	QueueName      string
	JobType        string
	Payload        json.RawMessage
	Priority       *int32
	MaxAttempts    *int32
	ScheduledAt    time.Time
	IdempotencyKey string
}

// JobSubmitter enqueues follow-up jobs. Direct workers write through the job
// service; remote workers post to the API.
type JobSubmitter interface {
	SubmitJob(ctx context.Context, input SubmitJobInput) (uuid.UUID, error)
}

// DirectSubmitter enqueues jobs through PostgreSQL for direct-mode workers.
type DirectSubmitter struct {
	service   *job.Service
	projectID uuid.UUID
}

func NewDirectSubmitter(service *job.Service, projectID uuid.UUID) *DirectSubmitter {
	return &DirectSubmitter{service: service, projectID: projectID}
}

func (d *DirectSubmitter) SubmitJob(ctx context.Context, input SubmitJobInput) (uuid.UUID, error) {
	result, err := d.service.Submit(ctx, job.SubmitInput{
		ProjectID:      d.projectID,
		QueueName:      input.QueueName,
		JobType:        input.JobType,
		Payload:        input.Payload,
		Priority:       input.Priority,
		MaxAttempts:    input.MaxAttempts,
		ScheduledAt:    input.ScheduledAt,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return result.Job.ID, nil
}

// APISubmitter enqueues jobs through the worker API, keeping remote workers
// free of database access.
type APISubmitter struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAPISubmitter(baseURL, token string, client *http.Client) *APISubmitter {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &APISubmitter{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

func (a *APISubmitter) SubmitJob(ctx context.Context, input SubmitJobInput) (uuid.UUID, error) {
	body := struct {
		QueueName   string          `json:"queue_name"`
		JobType     string          `json:"job_type"`
		Payload     json.RawMessage `json:"payload"`
		Priority    *int32          `json:"priority,omitempty"`
		MaxAttempts *int32          `json:"max_attempts,omitempty"`
		ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
	}{
		QueueName:   input.QueueName,
		JobType:     input.JobType,
		Payload:     input.Payload,
		Priority:    input.Priority,
		MaxAttempts: input.MaxAttempts,
	}
	if !input.ScheduledAt.IsZero() {
		scheduled := input.ScheduledAt
		body.ScheduledAt = &scheduled
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return uuid.Nil, fmt.Errorf("encode worker job request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/workers/jobs", bytes.NewReader(encoded))
	if err != nil {
		return uuid.Nil, fmt.Errorf("build worker job request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+a.token)
	if input.IdempotencyKey != "" {
		request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	}

	response, err := a.client.Do(request)
	if err != nil {
		return uuid.Nil, fmt.Errorf("worker job request: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return uuid.Nil, fmt.Errorf("read worker job response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error != "" {
			return uuid.Nil, fmt.Errorf("submit follow-up job: %s", failure.Error)
		}
		return uuid.Nil, fmt.Errorf("submit follow-up job: unexpected status %d", response.StatusCode)
	}

	var decoded struct {
		Job struct {
			ID uuid.UUID `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return uuid.Nil, fmt.Errorf("decode worker job response: %w", err)
	}
	if decoded.Job.ID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("submit follow-up job: response did not include a job id")
	}
	return decoded.Job.ID, nil
}

type delayPayload struct {
	DelaySeconds int64       `json:"delay_seconds"`
	RunAt        *time.Time  `json:"run_at"`
	Job          delayTarget `json:"job"`
}

type delayTarget struct {
	JobType     string          `json:"job_type"`
	QueueName   string          `json:"queue_name"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int32          `json:"priority"`
	MaxAttempts *int32          `json:"max_attempts"`
}

type delayResult struct {
	JobID       uuid.UUID `json:"job_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// NewDelayHandler schedules another job for later. Because the follow-up uses
// "delay:<job-id>" as its idempotency key, retrying the delay job never creates
// a duplicate target.
func NewDelayHandler(submitter JobSubmitter) execution.HandlerFunc {
	return func(ctx context.Context, jobRecord db.Job) (json.RawMessage, error) {
		if submitter == nil {
			return nil, execution.Permanent(fmt.Errorf("delay handler is not configured"))
		}

		var payload delayPayload
		if err := json.Unmarshal(jobRecord.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode delay.run payload: %w", err))
		}

		scheduledAt, err := resolveDelayTime(payload, time.Now())
		if err != nil {
			return nil, execution.Permanent(err)
		}

		targetType := strings.TrimSpace(payload.Job.JobType)
		if targetType == "" || len(targetType) > 100 {
			return nil, execution.Permanent(fmt.Errorf("job.job_type must be between 1 and 100 characters"))
		}
		queueName := strings.TrimSpace(payload.Job.QueueName)
		if len(queueName) > 100 {
			return nil, execution.Permanent(fmt.Errorf("job.queue_name must be at most 100 characters"))
		}
		if payload.Job.MaxAttempts != nil && (*payload.Job.MaxAttempts < 1 || *payload.Job.MaxAttempts > 100) {
			return nil, execution.Permanent(fmt.Errorf("job.max_attempts must be between 1 and 100"))
		}

		targetPayload := payload.Job.Payload
		if len(targetPayload) == 0 {
			targetPayload = json.RawMessage(`{}`)
		}

		jobID, err := submitter.SubmitJob(ctx, SubmitJobInput{
			QueueName:      queueName,
			JobType:        targetType,
			Payload:        targetPayload,
			Priority:       payload.Job.Priority,
			MaxAttempts:    payload.Job.MaxAttempts,
			ScheduledAt:    scheduledAt,
			IdempotencyKey: "delay:" + jobRecord.ID.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("schedule follow-up job: %w", err)
		}

		encoded, err := json.Marshal(delayResult{JobID: jobID, ScheduledAt: scheduledAt})
		if err != nil {
			return nil, fmt.Errorf("encode delay.run result: %w", err)
		}
		return encoded, nil
	}
}

func resolveDelayTime(payload delayPayload, now time.Time) (time.Time, error) {
	if payload.RunAt != nil && payload.DelaySeconds != 0 {
		return time.Time{}, fmt.Errorf("delay_seconds and run_at cannot both be set")
	}
	if payload.RunAt != nil {
		runAt := payload.RunAt.UTC()
		if runAt.After(now.Add(maxScheduleAhead)) {
			return time.Time{}, fmt.Errorf("run_at must be within one year")
		}
		if runAt.Before(now) {
			return now, nil
		}
		return runAt, nil
	}
	if payload.DelaySeconds < 0 {
		return time.Time{}, fmt.Errorf("delay_seconds must not be negative")
	}
	if payload.DelaySeconds > maxDelaySeconds {
		return time.Time{}, fmt.Errorf("delay_seconds must be at most %d", maxDelaySeconds)
	}
	return now.Add(time.Duration(payload.DelaySeconds) * time.Second), nil
}
