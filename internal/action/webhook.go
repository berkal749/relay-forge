package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
)

const (
	maxResponseBytes  = 64 << 10
	maxResultBodySize = 16 << 10
	maxRequestHeaders = 50
	maxHeaderValue    = 8 << 10
)

var templatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

var allowedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

type webhookPayload struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           json.RawMessage   `json:"body"`
	BodyTemplate   string            `json:"body_template"`
	ContentType    string            `json:"content_type"`
	Variables      map[string]any    `json:"variables"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	AllowNon2xx    bool              `json:"allow_non_2xx"`
}

type webhookResult struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
	DurationMS int64             `json:"duration_ms"`
}

// NewWebhookHandler calls a customer URL with the job payload and returns the
// response. The job-level retry policy handles retryable failures; permanent
// failures (malformed payloads, most 4xx responses) stop immediately.
func NewWebhookHandler(client *http.Client) execution.HandlerFunc {
	return func(ctx context.Context, job db.Job) (json.RawMessage, error) {
		var payload webhookPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, execution.Permanent(fmt.Errorf("decode http.request payload: %w", err))
		}

		variables := templateVariables(job)
		for key, value := range payload.Variables {
			variables[key] = value
		}

		rawURL, err := renderTemplate(payload.URL, variables)
		if err != nil {
			return nil, execution.Permanent(err)
		}
		target, err := parseTargetURL(rawURL)
		if err != nil {
			return nil, execution.Permanent(err)
		}

		method := strings.ToUpper(strings.TrimSpace(payload.Method))
		if method == "" {
			method = http.MethodPost
		}
		if !allowedMethods[method] {
			return nil, execution.Permanent(fmt.Errorf("unsupported http method %q", payload.Method))
		}

		body, contentType, err := buildRequestBody(payload, variables)
		if err != nil {
			return nil, execution.Permanent(err)
		}
		if (method == http.MethodGet || method == http.MethodHead) && len(body) > 0 {
			return nil, execution.Permanent(fmt.Errorf("%s requests cannot include a body", method))
		}

		requestCtx := ctx
		if payload.TimeoutSeconds > 0 {
			if payload.TimeoutSeconds > 300 {
				return nil, execution.Permanent(fmt.Errorf("timeout_seconds must be at most 300"))
			}
			var cancel context.CancelFunc
			requestCtx, cancel = context.WithTimeout(ctx, time.Duration(payload.TimeoutSeconds)*time.Second)
			defer cancel()
		}

		var reader io.Reader
		if len(body) > 0 {
			reader = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(requestCtx, method, target.String(), reader)
		if err != nil {
			return nil, execution.Permanent(fmt.Errorf("build request: %w", err))
		}
		request.Header.Set("User-Agent", "RelayForge/1.0")
		for key, value := range payload.Headers {
			if err := validateHeader(key, value); err != nil {
				return nil, execution.Permanent(err)
			}
			rendered, err := renderTemplate(value, variables)
			if err != nil {
				return nil, execution.Permanent(err)
			}
			request.Header.Set(key, rendered)
		}
		if len(body) > 0 && request.Header.Get("Content-Type") == "" {
			if contentType != "" {
				request.Header.Set("Content-Type", contentType)
			}
		}

		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("http request failed: %w", err)
		}
		defer response.Body.Close()

		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		truncated := len(responseBody) > maxResponseBytes
		if truncated {
			responseBody = responseBody[:maxResponseBytes]
		}
		if readErr != nil {
			return nil, fmt.Errorf("read http response: %w", readErr)
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if payload.AllowNon2xx {
				return encodeWebhookResult(response, responseBody, started, truncated)
			}
			return nil, statusError("http.request", response.StatusCode, responseBody)
		}

		return encodeWebhookResult(response, responseBody, started, truncated)
	}
}

func encodeWebhookResult(response *http.Response, body []byte, started time.Time, truncated bool) (json.RawMessage, error) {
	resultBody := body
	if len(resultBody) > maxResultBodySize {
		resultBody = resultBody[:maxResultBodySize]
		truncated = true
	}

	headers := make(map[string]string, len(response.Header))
	count := 0
	for key, values := range response.Header {
		if count >= maxRequestHeaders {
			break
		}
		lower := strings.ToLower(key)
		if lower == "set-cookie" || lower == "content-length" {
			continue
		}
		headers[key] = strings.Join(values, ", ")
		count++
	}

	result := webhookResult{
		Status:     response.StatusCode,
		Headers:    headers,
		Body:       string(resultBody),
		Truncated:  truncated,
		DurationMS: time.Since(started).Milliseconds(),
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode http.request result: %w", err)
	}
	return encoded, nil
}

func buildRequestBody(payload webhookPayload, variables map[string]any) ([]byte, string, error) {
	hasJSONBody := len(payload.Body) > 0 && string(payload.Body) != "null"
	if hasJSONBody && strings.TrimSpace(payload.BodyTemplate) != "" {
		return nil, "", fmt.Errorf("body and body_template cannot both be set")
	}
	if hasJSONBody {
		if !json.Valid(payload.Body) {
			return nil, "", fmt.Errorf("body must be valid JSON")
		}
		contentType := strings.TrimSpace(payload.ContentType)
		if contentType == "" {
			contentType = "application/json"
		}
		return payload.Body, contentType, nil
	}
	if strings.TrimSpace(payload.BodyTemplate) != "" {
		rendered, err := renderTemplate(payload.BodyTemplate, variables)
		if err != nil {
			return nil, "", err
		}
		contentType := strings.TrimSpace(payload.ContentType)
		if contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		return []byte(rendered), contentType, nil
	}
	return nil, "", nil
}

func parseTargetURL(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("url must use http or https")
	}
	if target.Host == "" {
		return nil, fmt.Errorf("url must include a host")
	}
	if target.User != nil {
		return nil, fmt.Errorf("url must not contain embedded credentials")
	}
	return target, nil
}

func validateHeader(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("header name must not be empty")
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return fmt.Errorf("invalid header name %q", key)
	}
	switch strings.ToLower(key) {
	case "host", "content-length", "transfer-encoding", "connection":
		return fmt.Errorf("header %q cannot be overridden", key)
	}
	if len(value) > maxHeaderValue {
		return fmt.Errorf("header %q is too large", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("header %q contains a newline", key)
	}
	return nil
}

func templateVariables(job db.Job) map[string]any {
	return map[string]any{
		"job_id":       job.ID.String(),
		"project_id":   job.ProjectID.String(),
		"job_type":     job.JobType,
		"queue_name":   job.QueueName,
		"attempt":      job.Attempts,
		"max_attempts": job.MaxAttempts,
	}
}

func renderTemplate(template string, variables map[string]any) (string, error) {
	var missing string
	rendered := templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		submatch := templatePattern.FindStringSubmatch(match)
		value, ok := variables[submatch[1]]
		if !ok {
			missing = submatch[1]
			return match
		}
		return templateValue(value)
	})
	if missing != "" {
		return "", fmt.Errorf("template variable %q is not defined", missing)
	}
	return rendered, nil
}

func templateValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded[1 : len(encoded)-1])
	case json.RawMessage:
		return string(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}
