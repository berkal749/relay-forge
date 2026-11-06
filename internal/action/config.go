package action

import (
	"fmt"
	"strings"

	"relayforge/internal/config"
)

// SMTPConfig describes the operator-provided email relay.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      string
}

// TwilioConfig holds the operator's Twilio credentials for SMS.
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
	FromNumber string
	// BaseURL overrides the Twilio API host. It exists for tests.
	BaseURL string
}

// Config carries operator credentials and outbound networking policy.
type Config struct {
	Network NetworkConfig

	SMTP              SMTPConfig
	Twilio            TwilioConfig
	TelegramBotToken  string
	TelegramAPIBase   string
	SlackWebhookURL   string
	DiscordWebhookURL string
}

// FromConfig builds action configuration from the process environment.
func FromConfig(cfg config.Config) Config {
	return Config{
		Network: NetworkConfig{
			EgressAllowlist:    cfg.WorkerEgressAllowlist,
			AllowPrivateEgress: cfg.WorkerAllowPrivateEgress,
			Timeout:            cfg.ActionHTTPTimeout,
		},
		SMTP: SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			TLS:      cfg.SMTPTLS,
		},
		Twilio: TwilioConfig{
			AccountSID: cfg.TwilioAccountSID,
			AuthToken:  cfg.TwilioAuthToken,
			FromNumber: cfg.TwilioFromNumber,
		},
		TelegramBotToken:  cfg.TelegramBotToken,
		SlackWebhookURL:   cfg.SlackWebhookURL,
		DiscordWebhookURL: cfg.DiscordWebhookURL,
	}
}

func (c Config) Validate() error {
	if c.SMTP.Host != "" {
		if c.SMTP.Port <= 0 {
			return fmt.Errorf("SMTP_PORT is required when SMTP_HOST is set")
		}
		if strings.TrimSpace(c.SMTP.From) == "" {
			return fmt.Errorf("SMTP_FROM is required when SMTP_HOST is set")
		}
	}
	if c.Network.Timeout <= 0 {
		return fmt.Errorf("action HTTP timeout must be positive")
	}
	return nil
}
