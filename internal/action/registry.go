package action

import (
	"relayforge/internal/execution"
)

// Job types provided by the built-in handlers.
const (
	JobTypeHTTPRequest = "http.request"
	JobTypeDelay       = "delay.run"
	JobTypeEmail       = "notify.email"
	JobTypeSlack       = "notify.slack"
	JobTypeDiscord     = "notify.discord"
	JobTypeTelegram    = "notify.telegram"
	JobTypeSMS         = "notify.sms"
)

// NewRegistry builds the worker handler registry from operator configuration.
func NewRegistry(config Config, submitter JobSubmitter) (*execution.Registry, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client, err := NewHTTPClient(config.Network)
	if err != nil {
		return nil, err
	}

	registry := execution.NewRegistry()
	registry.Register("echo", execution.Echo)
	registry.Register("noop", execution.Noop)
	registry.Register("sleep", execution.Sleep)
	registry.Register(JobTypeHTTPRequest, NewWebhookHandler(client))
	registry.Register(JobTypeDelay, NewDelayHandler(submitter))
	registry.Register(JobTypeEmail, NewEmailHandler(config.SMTP))
	registry.Register(JobTypeSlack, NewSlackHandler(client, config.SlackWebhookURL))
	registry.Register(JobTypeDiscord, NewDiscordHandler(client, config.DiscordWebhookURL))
	registry.Register(JobTypeTelegram, NewTelegramHandler(client, config.TelegramBotToken, config.TelegramAPIBase))
	registry.Register(JobTypeSMS, NewSMSHandler(client, config.Twilio))
	return registry, nil
}
