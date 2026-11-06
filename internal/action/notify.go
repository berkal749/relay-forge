package action

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/notificationchannel"
)

const (
	maxNotificationBody = 256 << 10
	maxEmailRecipients  = 50
	smtpDialTimeout     = 10 * time.Second
	twilioMessagesURL   = "https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json"
	telegramMessagesURL = "https://api.telegram.org/bot%s/sendMessage"
)

// stringList accepts either a JSON string or an array of strings.
type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("expected a string or an array of strings")
	}
	*s = list
	return nil
}

type emailPayload struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	To        stringList `json:"to"`
	CC        stringList `json:"cc"`
	BCC       stringList `json:"bcc"`
	Subject   string     `json:"subject"`
	Text      string     `json:"text"`
	HTML      string     `json:"html"`
	ReplyTo   string     `json:"reply_to"`
}

type notificationPayload struct {
	WebhookURL  string          `json:"webhook_url"`
	Text        string          `json:"text"`
	Content     string          `json:"content"`
	Blocks      json.RawMessage `json:"blocks"`
	Attachments json.RawMessage `json:"attachments"`
	Embeds      json.RawMessage `json:"embeds"`
	Username    string          `json:"username"`
	IconEmoji   string          `json:"icon_emoji"`
	AvatarURL   string          `json:"avatar_url"`
	ChatID      string          `json:"chat_id"`
	ParseMode   string          `json:"parse_mode"`
	DisableLink bool            `json:"disable_web_page_preview"`
	To          string          `json:"to"`
	Body        string          `json:"body"`
	From        string          `json:"from"`
	ChannelID   uuid.UUID       `json:"channel_id"`
}

type notificationResult struct {
	Channel string `json:"channel"`
	Status  int    `json:"status,omitempty"`
	OK      bool   `json:"ok"`
}

// NewEmailHandler delivers email through the operator's SMTP relay.
func NewEmailHandler(config SMTPConfig, resolver ChannelResolver) execution.HandlerFunc {
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload emailPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode notify.email payload: %w", err))
		}
		if payload.ChannelID != uuid.Nil {
			channel, err := resolveChannel(ctx, resolver, job.ProjectID, payload.ChannelID, notificationchannel.KindEmail)
			if err != nil {
				return nil, err
			}
			if len(payload.To) == 0 {
				payload.To = splitCSV(channel.Config["to"])
			}
		}
		if err := sendEmail(ctx, config, payload); err != nil {
			return nil, err
		}
		return encodeNotificationResult(notificationResult{Channel: "email", OK: true})
	}
}

// NewSlackHandler posts a message to a Slack incoming webhook.
func NewSlackHandler(client *http.Client, defaultWebhook string, resolver ChannelResolver) execution.HandlerFunc {
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload notificationPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode notify.slack payload: %w", err))
		}
		if payload.ChannelID != uuid.Nil {
			channel, err := resolveChannel(ctx, resolver, job.ProjectID, payload.ChannelID, notificationchannel.KindSlack)
			if err != nil {
				return nil, err
			}
			if payload.WebhookURL == "" {
				payload.WebhookURL = channel.Config["webhook_url"]
			}
		}
		webhook := firstNonEmpty(payload.WebhookURL, defaultWebhook)
		if webhook == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.slack requires webhook_url or SLACK_WEBHOOK_URL"))
		}
		body := map[string]any{}
		text := firstNonEmpty(payload.Text, payload.Content)
		if text == "" && len(payload.Blocks) == 0 {
			return nil, execution.Permanent(fmt.Errorf("notify.slack requires text, content, or blocks"))
		}
		if text != "" {
			body["text"] = text
		}
		if len(payload.Blocks) > 0 {
			body["blocks"] = payload.Blocks
		}
		if len(payload.Attachments) > 0 {
			body["attachments"] = payload.Attachments
		}
		if payload.Username != "" {
			body["username"] = payload.Username
		}
		if payload.IconEmoji != "" {
			body["icon_emoji"] = payload.IconEmoji
		}
		status, responseBody, err := postJSON(ctx, client, webhook, body)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, statusError("slack", status, responseBody)
		}
		return encodeNotificationResult(notificationResult{Channel: "slack", Status: status, OK: true})
	}
}

// NewDiscordHandler posts a message to a Discord webhook.
func NewDiscordHandler(client *http.Client, defaultWebhook string, resolver ChannelResolver) execution.HandlerFunc {
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload notificationPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode notify.discord payload: %w", err))
		}
		if payload.ChannelID != uuid.Nil {
			channel, err := resolveChannel(ctx, resolver, job.ProjectID, payload.ChannelID, notificationchannel.KindDiscord)
			if err != nil {
				return nil, err
			}
			if payload.WebhookURL == "" {
				payload.WebhookURL = channel.Config["webhook_url"]
			}
		}
		webhook := firstNonEmpty(payload.WebhookURL, defaultWebhook)
		if webhook == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.discord requires webhook_url or DISCORD_WEBHOOK_URL"))
		}
		body := map[string]any{}
		content := firstNonEmpty(payload.Content, payload.Text)
		if content == "" && len(payload.Embeds) == 0 {
			return nil, execution.Permanent(fmt.Errorf("notify.discord requires content or embeds"))
		}
		if content != "" {
			body["content"] = content
		}
		if len(payload.Embeds) > 0 {
			body["embeds"] = payload.Embeds
		}
		if payload.Username != "" {
			body["username"] = payload.Username
		}
		if payload.AvatarURL != "" {
			body["avatar_url"] = payload.AvatarURL
		}
		status, responseBody, err := postJSON(ctx, client, webhook, body)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, statusError("discord", status, responseBody)
		}
		return encodeNotificationResult(notificationResult{Channel: "discord", Status: status, OK: true})
	}
}

// NewTelegramHandler sends a message through the Telegram Bot API.
func NewTelegramHandler(client *http.Client, botToken, apiBase string, resolver ChannelResolver) execution.HandlerFunc {
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	apiBase = strings.TrimRight(apiBase, "/")
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload notificationPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode notify.telegram payload: %w", err))
		}
		if payload.ChannelID != uuid.Nil {
			channel, err := resolveChannel(ctx, resolver, job.ProjectID, payload.ChannelID, notificationchannel.KindTelegram)
			if err != nil {
				return nil, err
			}
			if payload.ChatID == "" {
				payload.ChatID = channel.Config["chat_id"]
			}
		}
		if strings.TrimSpace(botToken) == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.telegram requires TELEGRAM_BOT_TOKEN"))
		}
		chatID := strings.TrimSpace(payload.ChatID)
		if chatID == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.telegram requires chat_id"))
		}
		text := firstNonEmpty(payload.Text, payload.Content)
		if text == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.telegram requires text"))
		}
		body := map[string]any{
			"chat_id": chatID,
			"text":    text,
		}
		if payload.ParseMode != "" {
			body["parse_mode"] = payload.ParseMode
		}
		if payload.DisableLink {
			body["disable_web_page_preview"] = true
		}
		endpoint := apiBase + "/bot" + botToken + "/sendMessage"
		status, responseBody, err := postJSON(ctx, client, endpoint, body)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, statusError("telegram", status, responseBody)
		}
		return encodeNotificationResult(notificationResult{Channel: "telegram", Status: status, OK: true})
	}
}

// NewSMSHandler sends an SMS through Twilio.
func NewSMSHandler(client *http.Client, config TwilioConfig, resolver ChannelResolver) execution.HandlerFunc {
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload notificationPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode notify.sms payload: %w", err))
		}
		if payload.ChannelID != uuid.Nil {
			channel, err := resolveChannel(ctx, resolver, job.ProjectID, payload.ChannelID, notificationchannel.KindSMS)
			if err != nil {
				return nil, err
			}
			if payload.To == "" {
				payload.To = channel.Config["to"]
			}
			if payload.From == "" {
				payload.From = channel.Config["from"]
			}
		}
		if config.AccountSID == "" || config.AuthToken == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.sms requires TWILIO_ACCOUNT_SID and TWILIO_AUTH_TOKEN"))
		}
		to := strings.TrimSpace(payload.To)
		if to == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.sms requires to"))
		}
		bodyText := firstNonEmpty(payload.Body, payload.Text)
		if bodyText == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.sms requires body"))
		}
		from := firstNonEmpty(payload.From, config.FromNumber)
		if from == "" {
			return nil, execution.Permanent(fmt.Errorf("notify.sms requires from or TWILIO_FROM_NUMBER"))
		}

		form := url.Values{}
		form.Set("To", to)
		form.Set("From", from)
		form.Set("Body", bodyText)

		endpoint := fmt.Sprintf(twilioMessagesURL, config.AccountSID)
		if config.BaseURL != "" {
			endpoint = strings.TrimRight(config.BaseURL, "/") + "/2010-04-01/Accounts/" + config.AccountSID + "/Messages.json"
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, fmt.Errorf("build twilio request: %w", err)
		}
		request.SetBasicAuth(config.AccountSID, config.AuthToken)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("twilio request failed: %w", err)
		}
		defer response.Body.Close()
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxNotificationBody))
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, statusError("twilio", response.StatusCode, responseBody)
		}
		return encodeNotificationResult(notificationResult{Channel: "sms", Status: response.StatusCode, OK: true})
	}
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, body map[string]any) (int, []byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, nil, execution.Permanent(fmt.Errorf("encode notification: %w", err))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, execution.Permanent(fmt.Errorf("build notification request: %w", err))
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "RelayForge/1.0")

	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("notification request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxNotificationBody))
	return response.StatusCode, responseBody, nil
}

func encodeNotificationResult(result notificationResult) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode notification result: %w", err)
	}
	return encoded, nil
}

func sendEmail(ctx context.Context, config SMTPConfig, payload emailPayload) error {
	if strings.TrimSpace(config.Host) == "" {
		return execution.Permanent(fmt.Errorf("notify.email requires SMTP_HOST"))
	}
	if strings.TrimSpace(config.From) == "" {
		return execution.Permanent(fmt.Errorf("notify.email requires SMTP_FROM"))
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil {
		return execution.Permanent(fmt.Errorf("invalid SMTP_FROM: %w", err))
	}
	subject := strings.TrimSpace(payload.Subject)
	if subject == "" {
		return execution.Permanent(fmt.Errorf("notify.email requires subject"))
	}
	if strings.ContainsAny(subject, "\r\n") {
		return execution.Permanent(fmt.Errorf("subject must not contain newlines"))
	}
	if payload.Text == "" && payload.HTML == "" {
		return execution.Permanent(fmt.Errorf("notify.email requires text or html"))
	}
	if len(payload.Text) > maxNotificationBody || len(payload.HTML) > maxNotificationBody {
		return execution.Permanent(fmt.Errorf("email body is too large"))
	}

	recipients, err := parseRecipients(payload.To)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return execution.Permanent(fmt.Errorf("notify.email requires at least one recipient"))
	}
	cc, err := parseRecipients(payload.CC)
	if err != nil {
		return err
	}
	bcc, err := parseRecipients(payload.BCC)
	if err != nil {
		return err
	}
	all := append(append(append([]string{}, recipients...), cc...), bcc...)
	if len(all) > maxEmailRecipients {
		return execution.Permanent(fmt.Errorf("too many email recipients"))
	}

	message, err := buildEmailMessage(from, recipients, cc, subject, payload)
	if err != nil {
		return execution.Permanent(err)
	}

	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to smtp server: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	tlsConfig := &tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}
	if config.TLS == "tls" {
		conn = tls.Client(conn, tlsConfig)
	}

	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		return fmt.Errorf("start smtp client: %w", err)
	}
	defer client.Close()

	if config.TLS != "tls" && config.TLS != "none" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}

	if config.Username != "" {
		auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
		if err := client.Auth(auth); err != nil {
			return smtpFailure("smtp authentication", err)
		}
	}

	if err := client.Mail(from.Address); err != nil {
		return smtpFailure("smtp mail from", err)
	}
	for _, recipient := range all {
		if err := client.Rcpt(recipient); err != nil {
			return smtpFailure("smtp rcpt "+recipient, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		return fmt.Errorf("write smtp message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish smtp message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit smtp: %w", err)
	}
	return nil
}

// smtpFailure keeps 4xx SMTP replies retryable (greylisting, temporary
// throttling) while treating 5xx replies as terminal.
func smtpFailure(stage string, err error) error {
	wrapped := fmt.Errorf("%s: %w", stage, err)
	var protocolErr *textproto.Error
	if errors.As(err, &protocolErr) && protocolErr.Code >= 400 && protocolErr.Code < 500 {
		return wrapped
	}
	if errors.As(err, &protocolErr) {
		return execution.Permanent(wrapped)
	}
	return wrapped
}

func parseRecipients(list stringList) ([]string, error) {
	recipients := make([]string, 0, len(list))
	for _, raw := range list {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil {
			return nil, execution.Permanent(fmt.Errorf("invalid email recipient %q: %w", raw, err))
		}
		recipients = append(recipients, parsed.Address)
	}
	return recipients, nil
}

func buildEmailMessage(from *mail.Address, to, cc []string, subject string, payload emailPayload) ([]byte, error) {
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "From: %s\r\n", from.String())
	fmt.Fprintf(&buffer, "To: %s\r\n", strings.Join(to, ", "))
	if len(cc) > 0 {
		fmt.Fprintf(&buffer, "Cc: %s\r\n", strings.Join(cc, ", "))
	}
	fmt.Fprintf(&buffer, "Subject: %s\r\n", subject)
	if payload.ReplyTo != "" {
		if strings.ContainsAny(payload.ReplyTo, "\r\n") {
			return nil, fmt.Errorf("reply_to must not contain newlines")
		}
		fmt.Fprintf(&buffer, "Reply-To: %s\r\n", payload.ReplyTo)
	}
	fmt.Fprintf(&buffer, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprint(&buffer, "MIME-Version: 1.0\r\n")

	switch {
	case payload.HTML != "" && payload.Text != "":
		writer := multipart.NewWriter(&buffer)
		fmt.Fprintf(&buffer, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", writer.Boundary())
		textPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/plain; charset=UTF-8"}})
		if err != nil {
			return nil, err
		}
		fmt.Fprint(textPart, payload.Text)
		htmlPart, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"text/html; charset=UTF-8"}})
		if err != nil {
			return nil, err
		}
		fmt.Fprint(htmlPart, payload.HTML)
		if err := writer.Close(); err != nil {
			return nil, err
		}
	case payload.HTML != "":
		fmt.Fprint(&buffer, "Content-Type: text/html; charset=UTF-8\r\n\r\n")
		fmt.Fprint(&buffer, payload.HTML)
	default:
		fmt.Fprint(&buffer, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		fmt.Fprint(&buffer, payload.Text)
	}
	return buffer.Bytes(), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitCSV(value string) stringList {
	parts := strings.Split(value, ",")
	result := make(stringList, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
