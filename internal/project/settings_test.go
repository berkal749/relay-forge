package project

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

func int32Pointer(value int32) *int32    { return &value }
func stringPointer(value string) *string { return &value }

func TestUpdateInputParams(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()

	tests := []struct {
		name    string
		input   UpdateInput
		wantErr error
		check   func(t *testing.T, params *db.UpdateProjectParams)
	}{
		{
			name:    "empty update",
			input:   UpdateInput{},
			wantErr: ErrEmptyUpdate,
		},
		{
			name:    "invalid queue name",
			input:   UpdateInput{DefaultQueueName: stringPointer("   ")},
			wantErr: ErrInvalidDefaultQueueName,
		},
		{
			name:    "invalid default attempts",
			input:   UpdateInput{DefaultMaxAttempts: int32Pointer(0)},
			wantErr: ErrInvalidDefaultAttempts,
		},
		{
			name:    "invalid retention",
			input:   UpdateInput{JobRetentionDays: int32Pointer(-1)},
			wantErr: ErrInvalidRetentionDays,
		},
		{
			name:    "invalid concurrency",
			input:   UpdateInput{MaxConcurrency: int32Pointer(1001)},
			wantErr: ErrInvalidMaxConcurrency,
		},
		{
			name:    "invalid rate limit",
			input:   UpdateInput{RateLimitPerMinute: int32Pointer(1_000_001)},
			wantErr: ErrInvalidRateLimit,
		},
		{
			name:    "invalid job timeout",
			input:   UpdateInput{DefaultJobTimeoutSeconds: int32Pointer(86_401)},
			wantErr: ErrInvalidJobTimeout,
		},
		{
			name: "normalizes and maps all fields",
			input: UpdateInput{
				Name:                     stringPointer("  renamed  "),
				DefaultQueueName:         stringPointer("  critical  "),
				DefaultMaxAttempts:       int32Pointer(5),
				DefaultPriority:          int32Pointer(-2),
				JobRetentionDays:         int32Pointer(0),
				MaxConcurrency:           int32Pointer(8),
				RateLimitPerMinute:       int32Pointer(60),
				DefaultJobTimeoutSeconds: int32Pointer(30),
			},
			check: func(t *testing.T, params *db.UpdateProjectParams) {
				if *params.Name != "renamed" {
					t.Fatalf("Name = %q, want renamed", *params.Name)
				}
				if *params.DefaultQueueName != "critical" {
					t.Fatalf("DefaultQueueName = %q, want critical", *params.DefaultQueueName)
				}
				if *params.DefaultMaxAttempts != 5 {
					t.Fatalf("DefaultMaxAttempts = %d, want 5", *params.DefaultMaxAttempts)
				}
				if *params.DefaultPriority != -2 {
					t.Fatalf("DefaultPriority = %d, want -2", *params.DefaultPriority)
				}
				if *params.JobRetentionDays != 0 {
					t.Fatalf("JobRetentionDays = %d, want 0", *params.JobRetentionDays)
				}
				if *params.MaxConcurrency != 8 {
					t.Fatalf("MaxConcurrency = %d, want 8", *params.MaxConcurrency)
				}
				if *params.RateLimitPerMinute != 60 {
					t.Fatalf("RateLimitPerMinute = %d, want 60", *params.RateLimitPerMinute)
				}
				if *params.DefaultJobTimeoutSeconds != 30 {
					t.Fatalf("DefaultJobTimeoutSeconds = %d, want 30", *params.DefaultJobTimeoutSeconds)
				}
				if params.ID != projectID || params.UserID != userID {
					t.Fatalf("ids = %s/%s, want %s/%s", params.ID, params.UserID, projectID, userID)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params, err := test.input.params(projectID, userID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("params() error = %v, want %v", err, test.wantErr)
			}
			if test.check != nil {
				test.check(t, &params)
			}
		})
	}
}
