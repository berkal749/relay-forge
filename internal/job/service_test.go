package job

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizePayload(t *testing.T) {
	tests := []struct {
		name    string
		input   json.RawMessage
		want    string
		wantErr error
	}{
		{name: "defaults empty payload", input: nil, want: `{}`},
		{name: "accepts object", input: json.RawMessage(`{"user_id":"123"}`), want: `{"user_id":"123"}`},
		{name: "accepts array", input: json.RawMessage(`[1,2,3]`), want: `[1,2,3]`},
		{name: "trims whitespace", input: json.RawMessage("  {\"ok\":true}\n"), want: `{"ok":true}`},
		{name: "rejects null", input: json.RawMessage(`null`), wantErr: ErrInvalidPayload},
		{name: "rejects malformed JSON", input: json.RawMessage(`{"ok":`), wantErr: ErrInvalidPayload},
		{name: "rejects oversized payload", input: json.RawMessage(`"` + string(bytes.Repeat([]byte("a"), maxPayloadBytes)) + `"`), wantErr: ErrPayloadTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePayload(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("normalizePayload() error = %v, want %v", err, test.wantErr)
			}
			if string(got) != test.want {
				t.Fatalf("normalizePayload() = %s, want %s", got, test.want)
			}
		})
	}
}
