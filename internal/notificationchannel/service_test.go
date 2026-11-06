package notificationchannel

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeWebhookChannels(t *testing.T) {
	for _, kind := range []string{KindSlack, KindDiscord} {
		gotKind, config, err := normalize(kind, map[string]string{
			"webhook_url": "https://example.com/hook",
			"unused":      "ignored",
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if gotKind != kind {
			t.Fatalf("kind = %q, want %q", gotKind, kind)
		}
		if config["webhook_url"] != "https://example.com/hook" {
			t.Fatalf("webhook_url = %q", config["webhook_url"])
		}
		if _, exists := config["unused"]; exists {
			t.Fatal("unknown config keys should be dropped")
		}
	}

	if _, _, err := normalize(KindSlack, map[string]string{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing webhook error = %v", err)
	}
	if _, _, err := normalize(KindDiscord, map[string]string{"webhook_url": "ftp://example.com"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid scheme error = %v", err)
	}
	if _, _, err := normalize(KindDiscord, map[string]string{"webhook_url": "https://user:pass@example.com"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("embedded credentials error = %v", err)
	}
}

func TestNormalizeTelegram(t *testing.T) {
	_, config, err := normalize(KindTelegram, map[string]string{"chat_id": " -100123 "})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if config["chat_id"] != "-100123" {
		t.Fatalf("chat_id = %q", config["chat_id"])
	}
	if _, _, err := normalize(KindTelegram, map[string]string{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing chat_id error = %v", err)
	}
}

func TestNormalizeSMS(t *testing.T) {
	_, config, err := normalize(KindSMS, map[string]string{"to": "+15551234567", "from": "+15550000000"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if config["to"] != "+15551234567" || config["from"] != "+15550000000" {
		t.Fatalf("unexpected config: %+v", config)
	}
	if _, _, err := normalize(KindSMS, map[string]string{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing to error = %v", err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	_, config, err := normalize(KindEmail, map[string]string{
		"to": " one@example.com, two@example.com ",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if config["to"] != "one@example.com,two@example.com" {
		t.Fatalf("to = %q", config["to"])
	}
	if _, _, err := normalize(KindEmail, map[string]string{"to": "not-an-email"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid recipient error = %v", err)
	}
}

func TestNormalizeRejectsUnknownKind(t *testing.T) {
	if _, _, err := normalize("pager", map[string]string{}); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("unknown kind error = %v", err)
	}
}

func TestOnlyKeysDropsEmptyValues(t *testing.T) {
	config := onlyKeys(map[string]string{"to": "", "from": "x"}, "to", "from")
	if _, exists := config["to"]; exists {
		t.Fatal("empty value should be dropped")
	}
	if config["from"] != "x" {
		t.Fatal("non-empty value was dropped")
	}
}

func TestSplitRecipients(t *testing.T) {
	got := splitRecipients(" a@example.com, ,b@example.com ")
	if strings.Join(got, "|") != "a@example.com|b@example.com" {
		t.Fatalf("splitRecipients = %v", got)
	}
}
