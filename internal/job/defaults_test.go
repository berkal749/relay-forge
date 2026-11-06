package job

import (
	"errors"
	"testing"

	"relayforge/internal/database/db"
)

func TestResolveSubmitDefaults(t *testing.T) {
	settings := db.Project{
		DefaultQueueName:   "default",
		DefaultPriority:    7,
		DefaultMaxAttempts: 4,
	}

	tests := []struct {
		name            string
		settings        db.Project
		input           SubmitInput
		wantQueueName   string
		wantPriority    int32
		wantMaxAttempts int32
		wantErr         error
	}{
		{
			name:            "uses all project defaults",
			settings:        settings,
			input:           SubmitInput{},
			wantQueueName:   "default",
			wantPriority:    7,
			wantMaxAttempts: 4,
		},
		{
			name:     "explicit values win",
			settings: settings,
			input: SubmitInput{
				QueueName:   "  critical  ",
				Priority:    int32Ptr(99),
				MaxAttempts: int32Ptr(2),
			},
			wantQueueName:   "critical",
			wantPriority:    99,
			wantMaxAttempts: 2,
		},
		{
			name:            "project default queue is used after trimming",
			settings:        db.Project{DefaultQueueName: "  bulk  ", DefaultMaxAttempts: 1},
			input:           SubmitInput{QueueName: "   "},
			wantQueueName:   "bulk",
			wantPriority:    0,
			wantMaxAttempts: 1,
		},
		{
			name:     "invalid explicit attempts",
			settings: settings,
			input:    SubmitInput{MaxAttempts: int32Ptr(0)},
			wantErr:  ErrInvalidMaxAttempts,
		},
		{
			name:     "invalid queue name length",
			settings: settings,
			input:    SubmitInput{QueueName: string(make([]byte, maxQueueNameLength+1))},
			wantErr:  ErrInvalidQueueName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queueName, priority, maxAttempts, err := resolveSubmitDefaults(test.settings, test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("resolveSubmitDefaults() error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if queueName != test.wantQueueName {
				t.Fatalf("queueName = %q, want %q", queueName, test.wantQueueName)
			}
			if priority != test.wantPriority {
				t.Fatalf("priority = %d, want %d", priority, test.wantPriority)
			}
			if maxAttempts != test.wantMaxAttempts {
				t.Fatalf("maxAttempts = %d, want %d", maxAttempts, test.wantMaxAttempts)
			}
		})
	}
}

func int32Ptr(value int32) *int32 { return &value }
