package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
)

const remoteRequestTimeout = 20 * time.Second

var ErrRemoteUnauthorized = errors.New("worker token rejected by the API")

// RemoteConfig is the bootstrap payload returned by the API when a remote
// worker registers.
type RemoteConfig struct {
	WorkerID     uuid.UUID      `json:"worker_id"`
	ProjectID    uuid.UUID      `json:"project_id"`
	WorkerPool   *db.WorkerPool `json:"worker_pool"`
	LeaseSeconds int32          `json:"lease_seconds"`
	Project      struct {
		MaxConcurrency           int32 `json:"max_concurrency"`
		RateLimitPerMinute       int32 `json:"rate_limit_per_minute"`
		DefaultJobTimeoutSeconds int32 `json:"default_job_timeout_seconds"`
	} `json:"project"`
}

type RemoteIdentity struct {
	Token  string
	Config RemoteConfig
}

type remoteJob struct {
	ID          uuid.UUID       `json:"id"`
	ProjectID   uuid.UUID       `json:"project_id"`
	QueueName   string          `json:"queue_name"`
	JobType     string          `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Priority    int32           `json:"priority"`
	Attempts    int32           `json:"attempts"`
	MaxAttempts int32           `json:"max_attempts"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type remoteExecution struct {
	ID            uuid.UUID  `json:"id"`
	JobID         uuid.UUID  `json:"job_id"`
	WorkerID      *uuid.UUID `json:"worker_id"`
	AttemptNumber int32      `json:"attempt_number"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	ErrorMessage  *string    `json:"error_message"`
	Result        []byte     `json:"result"`
}

type remoteClaimResponse struct {
	Job       remoteJob       `json:"job"`
	Execution remoteExecution `json:"execution"`
}

type remoteFailureResponse struct {
	Retry     bool       `json:"retry"`
	RetryAt   *time.Time `json:"retry_at"`
	JobStatus string     `json:"job_status"`
}

// RemoteBackend talks to the API over HTTPS and never opens a database
// connection.
type RemoteBackend struct {
	baseURL  string
	client   *http.Client
	token    string
	workerID uuid.UUID
}

func NewRemoteBackend(baseURL, token string, workerID uuid.UUID, client *http.Client) *RemoteBackend {
	if client == nil {
		client = &http.Client{Timeout: remoteRequestTimeout}
	}
	return &RemoteBackend{
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   client,
		token:    token,
		workerID: workerID,
	}
}

func (b *RemoteBackend) Claim(ctx context.Context) (execution.Claim, error) {
	var response remoteClaimResponse
	if err := b.do(ctx, http.MethodPost, "/v1/workers/claim", map[string]any{"worker_id": b.workerID}, &response); err != nil {
		return execution.Claim{}, err
	}
	if response.Job.ID == uuid.Nil {
		return execution.Claim{}, execution.ErrNoJobs
	}

	payload := []byte(response.Job.Payload)
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	result := response.Execution.Result
	job := db.Job{
		ID:          response.Job.ID,
		ProjectID:   response.Job.ProjectID,
		QueueName:   response.Job.QueueName,
		JobType:     response.Job.JobType,
		Payload:     payload,
		Status:      response.Job.Status,
		Priority:    response.Job.Priority,
		Attempts:    response.Job.Attempts,
		MaxAttempts: response.Job.MaxAttempts,
		ScheduledAt: response.Job.ScheduledAt,
		CreatedAt:   response.Job.CreatedAt,
		UpdatedAt:   response.Job.UpdatedAt,
	}
	executionRecord := db.Execution{
		ID:            response.Execution.ID,
		JobID:         response.Execution.JobID,
		WorkerID:      response.Execution.WorkerID,
		AttemptNumber: response.Execution.AttemptNumber,
		Status:        response.Execution.Status,
		StartedAt:     response.Execution.StartedAt,
		FinishedAt:    response.Execution.FinishedAt,
		ErrorMessage:  response.Execution.ErrorMessage,
		Result:        result,
	}
	return execution.Claim{Job: job, Execution: executionRecord}, nil
}

func (b *RemoteBackend) Succeed(ctx context.Context, claim execution.Claim, result []byte) error {
	if len(result) == 0 {
		result = []byte(`{}`)
	}
	return b.do(ctx, http.MethodPost, "/v1/workers/jobs/"+claim.Job.ID.String()+"/complete", map[string]any{
		"execution_id": claim.Execution.ID,
		"result":       json.RawMessage(result),
	}, nil)
}

func (b *RemoteBackend) Fail(ctx context.Context, claim execution.Claim, cause error, cancelled bool) (execution.FailureOutcome, error) {
	message := "job failed"
	if cause != nil {
		message = cause.Error()
	}
	var response remoteFailureResponse
	if err := b.do(ctx, http.MethodPost, "/v1/workers/jobs/"+claim.Job.ID.String()+"/fail", map[string]any{
		"execution_id": claim.Execution.ID,
		"error":        message,
		"cancelled":    cancelled,
	}, &response); err != nil {
		return execution.FailureOutcome{}, err
	}

	outcome := execution.FailureOutcome{
		Retry:     response.Retry,
		JobStatus: response.JobStatus,
	}
	if response.RetryAt != nil {
		outcome.RetryAt = *response.RetryAt
	}
	return outcome, nil
}

func (b *RemoteBackend) Heartbeat(ctx context.Context) error {
	return b.do(ctx, http.MethodPost, "/v1/workers/heartbeat", map[string]any{"worker_id": b.workerID}, nil)
}

func (b *RemoteBackend) Offline(ctx context.Context) error {
	return b.do(ctx, http.MethodPost, "/v1/workers/offline", map[string]any{"worker_id": b.workerID}, nil)
}

func (b *RemoteBackend) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode remote worker request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, b.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build remote worker request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+b.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := b.client.Do(request)
	if err != nil {
		return fmt.Errorf("remote worker request: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read remote worker response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		return ErrRemoteUnauthorized
	}
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error != "" {
			return fmt.Errorf("remote worker request: %s", failure.Error)
		}
		return fmt.Errorf("remote worker request: unexpected status %d", response.StatusCode)
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode remote worker response: %w", err)
	}
	return nil
}

var _ Backend = (*RemoteBackend)(nil)

// ResolveRemoteIdentity finds a worker token, optionally exchanges an
// enrollment token, registers the worker with the API, and caches the issued
// token. It never touches PostgreSQL.
func ResolveRemoteIdentity(ctx context.Context, opts CredentialOptions) (RemoteIdentity, error) {
	if opts.APIBaseURL == "" {
		return RemoteIdentity{}, fmt.Errorf("RELAYFORGE_API_URL is required for a remote worker")
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: remoteRequestTimeout}
	}

	var rawToken string
	fromEnrollment := false
	if opts.Token != "" {
		rawToken = opts.Token
	} else if opts.TokenFile != "" {
		if stored, err := readStoredCredentials(opts.TokenFile); err == nil {
			rawToken = stored.Token
		}
	}

	enrollNow := func() error {
		enrolled, err := enroll(ctx, CredentialOptions{
			APIBaseURL:      opts.APIBaseURL,
			EnrollmentToken: opts.EnrollmentToken,
			HTTPClient:      client,
		})
		if err != nil {
			return err
		}
		rawToken = enrolled.Token
		fromEnrollment = true
		if opts.TokenFile != "" {
			if err := writeStoredCredentials(opts.TokenFile, storedCredentials{
				ProjectID: enrolled.ProjectID.String(),
				Token:     enrolled.Token,
			}); err != nil {
				return fmt.Errorf("cache worker token: %w", err)
			}
		}
		return nil
	}

	if rawToken == "" {
		if opts.EnrollmentToken == "" {
			return RemoteIdentity{}, fmt.Errorf("no remote worker credentials found: set WORKER_TOKEN, WORKER_ENROLLMENT_TOKEN, or WORKER_TOKEN_FILE")
		}
		if err := enrollNow(); err != nil {
			return RemoteIdentity{}, err
		}
	}

	register := func() (RemoteConfig, error) {
		return registerRemote(ctx, client, opts.APIBaseURL, rawToken, opts.WorkerName)
	}

	config, err := register()
	if errors.Is(err, ErrRemoteUnauthorized) && !fromEnrollment && opts.EnrollmentToken != "" {
		if enrollErr := enrollNow(); enrollErr != nil {
			return RemoteIdentity{}, enrollErr
		}
		config, err = register()
	}
	if err != nil {
		return RemoteIdentity{}, err
	}

	return RemoteIdentity{Token: rawToken, Config: config}, nil
}

func registerRemote(ctx context.Context, client *http.Client, baseURL, token, name string) (RemoteConfig, error) {
	requestBody, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return RemoteConfig{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/workers/register", bytes.NewReader(requestBody))
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("build worker registration request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("register remote worker: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("read worker registration response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		return RemoteConfig{}, ErrRemoteUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error != "" {
			return RemoteConfig{}, fmt.Errorf("register remote worker: %s", failure.Error)
		}
		return RemoteConfig{}, fmt.Errorf("register remote worker: unexpected status %d", response.StatusCode)
	}

	var config RemoteConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return RemoteConfig{}, fmt.Errorf("decode worker registration response: %w", err)
	}
	if config.WorkerID == uuid.Nil || config.ProjectID == uuid.Nil {
		return RemoteConfig{}, fmt.Errorf("register remote worker: incomplete response")
	}
	return config, nil
}
