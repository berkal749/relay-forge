package workerpool

import (
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trims", input: "  general  ", want: "general"},
		{name: "empty", input: "   ", wantErr: true},
		{name: "too long", input: string(make([]byte, 101)), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateName(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatal("validateName() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validateName() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("validateName() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateConcurrency(t *testing.T) {
	tests := []struct {
		input   int32
		wantErr bool
	}{
		{input: 1},
		{input: 4},
		{input: 1000},
		{input: 0, wantErr: true},
		{input: 1001, wantErr: true},
	}

	for _, test := range tests {
		err := validateConcurrency(test.input)
		if test.wantErr && err == nil {
			t.Fatalf("validateConcurrency(%d) expected error", test.input)
		}
		if !test.wantErr && err != nil {
			t.Fatalf("validateConcurrency(%d) error = %v", test.input, err)
		}
	}
}

func TestUpdateInputEmpty(t *testing.T) {
	if !(UpdateInput{}).Empty() {
		t.Fatal("empty UpdateInput should report empty")
	}
	name := "pool"
	if (UpdateInput{Name: &name}).Empty() {
		t.Fatal("UpdateInput with a field should not report empty")
	}
}

func TestNormalizeQueueNames(t *testing.T) {
	got, err := normalizeQueueNames([]string{" emails ", "default", "emails", ""})
	if err == nil {
		t.Fatal("normalizeQueueNames() expected error for an empty queue")
	}

	got, err = normalizeQueueNames([]string{" emails ", "default", "emails"})
	if err != nil {
		t.Fatalf("normalizeQueueNames() error = %v", err)
	}
	if len(got) != 2 || got[0] != "emails" || got[1] != "default" {
		t.Fatalf("normalizeQueueNames() = %v, want [emails default]", got)
	}

	if _, err := normalizeQueueNames(make([]string, 51)); err == nil {
		t.Fatal("normalizeQueueNames() expected error for too many queues")
	}
}

func TestValidatePolicy(t *testing.T) {
	tests := []struct {
		name     string
		backoff  string
		base     int32
		maxDelay int32
		wantErr  bool
	}{
		{name: "defaults", backoff: BackoffExponential},
		{name: "fixed", backoff: BackoffFixed, base: 5, maxDelay: 60},
		{name: "invalid backoff", backoff: "linear", wantErr: true},
		{name: "max below base", backoff: BackoffFixed, base: 60, maxDelay: 5, wantErr: true},
		{name: "negative base", backoff: BackoffFixed, base: -1, wantErr: true},
		{name: "timeout too high", backoff: BackoffExponential, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timeout := int32(0)
			if test.name == "timeout too high" {
				timeout = 86401
			}
			_, _, _, err := validatePolicy(nil, timeout, 0, test.backoff, test.base, test.maxDelay, nil)
			if test.wantErr && err == nil {
				t.Fatal("validatePolicy() expected error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validatePolicy() error = %v", err)
			}
		})
	}
}

func TestUpdateInputEmptyWithPolicyFields(t *testing.T) {
	queues := []string{"default"}
	timeout := int32(30)
	if (UpdateInput{QueueNames: &queues}).Empty() {
		t.Fatal("UpdateInput with queues should not report empty")
	}
	if (UpdateInput{JobTimeoutSeconds: &timeout}).Empty() {
		t.Fatal("UpdateInput with timeout should not report empty")
	}
}

func TestNormalizeCIDRs(t *testing.T) {
	got, err := normalizeCIDRs([]string{"203.0.113.7", "10.0.0.0/8", "10.0.0.0/8", "2001:db8::1"})
	if err != nil {
		t.Fatalf("normalizeCIDRs() error = %v", err)
	}
	want := []string{"203.0.113.7/32", "10.0.0.0/8", "2001:db8::1/128"}
	if len(got) != len(want) {
		t.Fatalf("normalizeCIDRs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeCIDRs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if _, err := normalizeCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Fatal("normalizeCIDRs() expected error for invalid CIDR")
	}
	if _, err := normalizeCIDRs(make([]string, 51)); err == nil {
		t.Fatal("normalizeCIDRs() expected error for too many entries")
	}
}
