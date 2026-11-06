package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relayforge/internal/database/db"
	"relayforge/internal/workertoken"
)

const enrollmentTimeout = 15 * time.Second

// Credentials are the resolved identity a worker uses at startup.
type Credentials struct {
	ProjectID uuid.UUID
	Token     db.WorkerToken
	Pool      *db.WorkerPool
}

// CredentialOptions describes where a worker should look for its identity.
// Explicit tokens win over the cached token file, which wins over enrollment.
type CredentialOptions struct {
	ProjectID       string
	Token           string
	TokenFile       string
	EnrollmentToken string
	APIBaseURL      string
	HTTPClient      *http.Client
}

type storedCredentials struct {
	ProjectID string `json:"project_id"`
	Token     string `json:"token"`
}

type enrollResponse struct {
	ProjectID   uuid.UUID      `json:"project_id"`
	Token       string         `json:"token"`
	WorkerPool  *db.WorkerPool `json:"worker_pool"`
	WorkerToken db.WorkerToken `json:"worker_token"`
}

// ResolveCredentials authenticates a worker using an explicit token, a cached
// token file, or a single-use enrollment token. Successful enrollment writes the
// issued token to TokenFile with owner-only permissions.
func ResolveCredentials(ctx context.Context, queries *db.Queries, tokens *workertoken.Service, opts CredentialOptions) (Credentials, error) {
	if opts.Token != "" {
		projectID, err := requireProjectID(opts.ProjectID)
		if err != nil {
			return Credentials{}, err
		}
		return authenticate(ctx, queries, tokens, projectID, opts.Token)
	}

	if opts.TokenFile != "" {
		stored, err := readStoredCredentials(opts.TokenFile)
		if err == nil {
			projectID, parseErr := uuid.Parse(stored.ProjectID)
			if parseErr == nil {
				credentials, authErr := authenticate(ctx, queries, tokens, projectID, stored.Token)
				if authErr == nil {
					return credentials, nil
				}
				if opts.EnrollmentToken == "" {
					return Credentials{}, fmt.Errorf("stored worker token is no longer valid (remove %s or set WORKER_ENROLLMENT_TOKEN): %w", opts.TokenFile, authErr)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) && opts.EnrollmentToken == "" {
			return Credentials{}, fmt.Errorf("read worker token file: %w", err)
		}
	}

	if opts.EnrollmentToken != "" {
		enrolled, err := enroll(ctx, opts)
		if err != nil {
			return Credentials{}, err
		}
		if opts.TokenFile != "" {
			if err := writeStoredCredentials(opts.TokenFile, storedCredentials{
				ProjectID: enrolled.ProjectID.String(),
				Token:     enrolled.Token,
			}); err != nil {
				return Credentials{}, fmt.Errorf("cache worker token: %w", err)
			}
		}
		credentials, err := authenticate(ctx, queries, tokens, enrolled.ProjectID, enrolled.Token)
		if err != nil {
			return Credentials{}, err
		}
		if enrolled.WorkerPool != nil {
			credentials.Pool = enrolled.WorkerPool
		}
		return credentials, nil
	}

	return Credentials{}, fmt.Errorf("no worker credentials found: set WORKER_TOKEN, WORKER_ENROLLMENT_TOKEN, or WORKER_TOKEN_FILE")
}

func authenticate(ctx context.Context, queries *db.Queries, tokens *workertoken.Service, projectID uuid.UUID, rawToken string) (Credentials, error) {
	token, err := tokens.Authenticate(ctx, projectID, rawToken)
	if err != nil {
		return Credentials{}, err
	}

	credentials := Credentials{ProjectID: projectID, Token: token}
	if token.PoolID != nil {
		pool, err := queries.GetWorkerPool(ctx, db.GetWorkerPoolParams{
			ID:        *token.PoolID,
			ProjectID: projectID,
		})
		switch {
		case err == nil:
			credentials.Pool = &pool
		case errors.Is(err, pgx.ErrNoRows):
			return Credentials{}, fmt.Errorf("worker pool for token no longer exists")
		default:
			return Credentials{}, fmt.Errorf("load worker pool: %w", err)
		}
	}
	return credentials, nil
}

func enroll(ctx context.Context, opts CredentialOptions) (enrollResponse, error) {
	if opts.APIBaseURL == "" {
		return enrollResponse{}, fmt.Errorf("RELAYFORGE_API_URL is required to enroll a worker")
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: enrollmentTimeout}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(opts.APIBaseURL, "/")+"/v1/workers/enroll", bytes.NewReader([]byte("{}")))
	if err != nil {
		return enrollResponse{}, fmt.Errorf("build enrollment request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+opts.EnrollmentToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return enrollResponse{}, fmt.Errorf("enroll worker: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return enrollResponse{}, fmt.Errorf("read enrollment response: %w", err)
	}
	if response.StatusCode != http.StatusCreated {
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
			return enrollResponse{}, fmt.Errorf("enroll worker: %s", payload.Error)
		}
		return enrollResponse{}, fmt.Errorf("enroll worker: unexpected status %d", response.StatusCode)
	}

	var enrolled enrollResponse
	if err := json.Unmarshal(body, &enrolled); err != nil {
		return enrollResponse{}, fmt.Errorf("decode enrollment response: %w", err)
	}
	if enrolled.Token == "" || enrolled.ProjectID == uuid.Nil {
		return enrollResponse{}, fmt.Errorf("enroll worker: incomplete response")
	}
	return enrolled, nil
}

func requireProjectID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, fmt.Errorf("WORKER_PROJECT_ID is required when WORKER_TOKEN is set")
	}
	projectID, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse WORKER_PROJECT_ID: %w", err)
	}
	return projectID, nil
}

func readStoredCredentials(path string) (storedCredentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return storedCredentials{}, err
	}
	var stored storedCredentials
	if err := json.Unmarshal(data, &stored); err != nil {
		return storedCredentials{}, fmt.Errorf("decode worker token file: %w", err)
	}
	if stored.Token == "" || stored.ProjectID == "" {
		return storedCredentials{}, fmt.Errorf("worker token file is missing project_id or token")
	}
	return stored, nil
}

func writeStoredCredentials(path string, stored storedCredentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".worker-token-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
