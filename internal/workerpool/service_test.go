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
