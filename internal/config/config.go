package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDatabaseURL = "postgres://relayforge:relayforge@localhost:5435/relayforge?sslmode=disable"
	defaultHTTPAddr    = ":8080"
	defaultSessionTTL  = 30 * 24 * time.Hour
)

type Config struct {
	AppEnv                 string
	DatabaseURL            string
	DatabaseConnectTimeout time.Duration
	HTTPAddr               string
	SessionTTL             time.Duration
	APIKeyPepper           string
	WorkerTokenPepper      string

	RateLimitAuthPerMinute      int
	RateLimitAPIPerMinute       int
	RateLimitJobSubmitPerMinute int

	RegistrationMode          string
	RegistrationModeExplicit  bool
	BootstrapAdminEmail       string
	InviteTTL                 time.Duration
	TrustProxyHeadersExplicit bool

	MaxProjectsPerUser             int
	MaxWorkerPoolsPerProject       int
	MaxActivePoolsPerProject       int
	MaxWorkerConcurrencyPerProject int
	MaxQueuedJobsPerProject        int

	MetricsAddr    string
	MetricsEnabled bool

	APISchedulerInterval   time.Duration
	MaxSchedulesPerProject int

	WorkerAllowPrivateEgress bool
	ActionHTTPTimeout        time.Duration

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTLS      string

	SlackWebhookURL   string
	DiscordWebhookURL string
	TelegramBotToken  string

	TwilioAccountSID string
	TwilioAuthToken  string
	TwilioFromNumber string

	DatabaseURLExplicit bool
	APIBaseURLExplicit  bool

	WorkerMode              string
	WorkerProjectID         string
	WorkerToken             string
	WorkerTokenFile         string
	WorkerEnrollmentToken   string
	WorkerEnrollmentTTL     time.Duration
	APIBaseURL              string
	WorkerName              string
	WorkerConcurrency       int
	WorkerPollInterval      time.Duration
	WorkerJobTimeout        time.Duration
	WorkerShutdownTimeout   time.Duration
	WorkerHeartbeatInterval time.Duration
	WorkerCleanupInterval   time.Duration
	RetryBaseDelay          time.Duration
	RetryMaxDelay           time.Duration
	WorkerLeaseGrace        time.Duration
	WorkerEgressAllowlist   []string
	WorkerSecretsFile       string

	TokenRotationGrace     time.Duration
	APIOutboxInterval      time.Duration
	APICleanupInterval     time.Duration
	APIReaperInterval      time.Duration
	APIWorkerSweepInterval time.Duration
	WorkerStaleAfter       time.Duration
	TrustProxyHeaders      bool
	WorkerMTLSHeader       string

	SupervisorPollInterval time.Duration
	SupervisorWorkerBinary string
	SupervisorTokenDir     string
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	databaseURLExplicit := databaseURL != ""
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL
	}

	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}

	sessionTTL, err := envDuration("SESSION_TTL", defaultSessionTTL)
	if err != nil {
		return Config{}, err
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}

	production := appEnv == "production"

	// Peppers are validated here when present. API-only production requirements
	// live in ValidateAPI so workers and the supervisor can start without them.
	apiKeyPepper := os.Getenv("API_KEY_PEPPER")
	if apiKeyPepper == "" && !production {
		apiKeyPepper = "relayforge-development-pepper"
	}
	minPepperLength := 16
	if production {
		minPepperLength = 32
	}
	if apiKeyPepper != "" && len(apiKeyPepper) < minPepperLength {
		return Config{}, fmt.Errorf("API_KEY_PEPPER must be at least %d characters", minPepperLength)
	}

	workerTokenPepper := os.Getenv("WORKER_TOKEN_PEPPER")
	if workerTokenPepper == "" {
		workerTokenPepper = apiKeyPepper
	}
	if workerTokenPepper != "" && len(workerTokenPepper) < minPepperLength {
		return Config{}, fmt.Errorf("WORKER_TOKEN_PEPPER must be at least %d characters", minPepperLength)
	}

	registrationMode := strings.ToLower(strings.TrimSpace(os.Getenv("REGISTRATION_MODE")))
	registrationModeExplicit := registrationMode != ""
	if registrationMode == "" {
		if production {
			registrationMode = "invite"
		} else {
			registrationMode = "open"
		}
	}
	if registrationMode != "open" && registrationMode != "invite" && registrationMode != "closed" {
		return Config{}, fmt.Errorf("REGISTRATION_MODE must be open, invite, or closed")
	}

	inviteTTL, err := envDuration("INVITE_TTL", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if inviteTTL < time.Hour || inviteTTL > 30*24*time.Hour {
		return Config{}, fmt.Errorf("INVITE_TTL must be between 1h and 720h")
	}

	rateLimitAuthPerMinute, err := envNonNegativeInt("RATE_LIMIT_AUTH_PER_MINUTE", 20)
	if err != nil {
		return Config{}, err
	}
	rateLimitAPIPerMinute, err := envNonNegativeInt("RATE_LIMIT_API_PER_MINUTE", 600)
	if err != nil {
		return Config{}, err
	}
	rateLimitJobSubmitPerMinute, err := envNonNegativeInt("RATE_LIMIT_JOB_SUBMIT_PER_MINUTE", 120)
	if err != nil {
		return Config{}, err
	}

	workerName := os.Getenv("WORKER_NAME")
	if workerName == "" {
		workerName = defaultWorkerName()
	}

	workerConcurrency, err := envInt("WORKER_CONCURRENCY", 4)
	if err != nil {
		return Config{}, err
	}
	if workerConcurrency < 1 || workerConcurrency > 1000 {
		return Config{}, fmt.Errorf("WORKER_CONCURRENCY must be between 1 and 1000")
	}

	workerPollInterval, err := envDuration("WORKER_POLL_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}
	workerJobTimeout, err := envDuration("JOB_TIMEOUT", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	workerShutdownTimeout, err := envDuration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerHeartbeatInterval, err := envDuration("WORKER_HEARTBEAT_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerCleanupInterval, err := envDuration("WORKER_CLEANUP_INTERVAL", time.Hour)
	if err != nil {
		return Config{}, err
	}
	retryBaseDelay, err := envDuration("RETRY_BASE_DELAY", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	retryMaxDelay, err := envDuration("RETRY_MAX_DELAY", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if retryMaxDelay < retryBaseDelay {
		return Config{}, fmt.Errorf("RETRY_MAX_DELAY must be greater than or equal to RETRY_BASE_DELAY")
	}

	workerMode := strings.ToLower(strings.TrimSpace(os.Getenv("WORKER_MODE")))
	if workerMode == "" {
		if databaseURLExplicit {
			workerMode = "direct"
		} else {
			workerMode = "remote"
		}
	}
	if workerMode != "direct" && workerMode != "remote" {
		return Config{}, fmt.Errorf("WORKER_MODE must be direct or remote")
	}

	workerLeaseGrace, err := envDuration("WORKER_LEASE_GRACE", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerTokenFile := os.Getenv("WORKER_TOKEN_FILE")
	if workerTokenFile == "" {
		workerTokenFile = ".relayforge/worker-token.json"
	}

	apiBaseURL := os.Getenv("RELAYFORGE_API_URL")
	apiBaseURLExplicit := apiBaseURL != ""
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080"
	}
	apiBaseURL = strings.TrimRight(apiBaseURL, "/")

	workerEnrollmentTTL, err := envDuration("WORKER_ENROLLMENT_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if workerEnrollmentTTL < time.Minute || workerEnrollmentTTL > 24*time.Hour {
		return Config{}, fmt.Errorf("WORKER_ENROLLMENT_TTL must be between 1m and 24h")
	}

	tokenRotationGrace, err := envDuration("TOKEN_ROTATION_GRACE", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	apiOutboxInterval, err := envDuration("API_OUTBOX_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}
	apiCleanupInterval, err := envDuration("API_CLEANUP_INTERVAL", time.Hour)
	if err != nil {
		return Config{}, err
	}
	apiReaperInterval, err := envDuration("API_REAPER_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	apiWorkerSweepInterval, err := envDuration("API_WORKER_SWEEP_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerStaleAfter, err := envDuration("WORKER_STALE_AFTER", 90*time.Second)
	if err != nil {
		return Config{}, err
	}
	trustProxyHeaders := false
	trustProxyValue := strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS"))
	trustProxyHeadersExplicit := trustProxyValue != ""
	if trustProxyValue != "" {
		trustProxyHeaders, err = strconv.ParseBool(trustProxyValue)
		if err != nil {
			return Config{}, fmt.Errorf("parse TRUST_PROXY_HEADERS: %w", err)
		}
	}
	workerEgressAllowlist := splitList(os.Getenv("WORKER_EGRESS_ALLOWLIST"))

	maxProjectsPerUser, err := envNonNegativeInt("MAX_PROJECTS_PER_USER", 5)
	if err != nil {
		return Config{}, err
	}
	maxWorkerPoolsPerProject, err := envNonNegativeInt("MAX_WORKER_POOLS_PER_PROJECT", 10)
	if err != nil {
		return Config{}, err
	}
	maxActivePoolsPerProject, err := envNonNegativeInt("MAX_ACTIVE_POOLS_PER_PROJECT", 5)
	if err != nil {
		return Config{}, err
	}
	maxWorkerConcurrencyPerProject, err := envNonNegativeInt("MAX_WORKER_CONCURRENCY_PER_PROJECT", 50)
	if err != nil {
		return Config{}, err
	}
	maxQueuedJobsPerProject, err := envNonNegativeInt("MAX_QUEUED_JOBS_PER_PROJECT", 100000)
	if err != nil {
		return Config{}, err
	}

	apiSchedulerInterval, err := envDuration("API_SCHEDULER_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	maxSchedulesPerProject, err := envNonNegativeInt("MAX_SCHEDULES_PER_PROJECT", 50)
	if err != nil {
		return Config{}, err
	}

	workerAllowPrivateEgress, err := envBool("WORKER_ALLOW_PRIVATE_EGRESS", false)
	if err != nil {
		return Config{}, err
	}
	actionHTTPTimeout, err := envDuration("ACTION_HTTP_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	if actionHTTPTimeout < time.Second || actionHTTPTimeout > 5*time.Minute {
		return Config{}, fmt.Errorf("ACTION_HTTP_TIMEOUT must be between 1s and 5m")
	}

	smtpPort, err := envInt("SMTP_PORT", 587)
	if err != nil {
		return Config{}, err
	}
	if smtpPort < 1 || smtpPort > 65535 {
		return Config{}, fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	smtpTLS := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS")))
	if smtpTLS == "" {
		smtpTLS = "starttls"
	}
	if smtpTLS != "starttls" && smtpTLS != "tls" && smtpTLS != "none" {
		return Config{}, fmt.Errorf("SMTP_TLS must be starttls, tls, or none")
	}

	metricsAddr := os.Getenv("METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9090"
	}
	metricsEnabled := true
	if value := strings.TrimSpace(os.Getenv("METRICS_ENABLED")); value != "" {
		metricsEnabled, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse METRICS_ENABLED: %w", err)
		}
	}

	supervisorPollInterval, err := envDuration("SUPERVISOR_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	supervisorWorkerBinary := os.Getenv("SUPERVISOR_WORKER_BINARY")
	if supervisorWorkerBinary == "" {
		supervisorWorkerBinary = "relayforge-worker"
	}
	supervisorTokenDir := os.Getenv("SUPERVISOR_TOKEN_DIR")
	if supervisorTokenDir == "" {
		supervisorTokenDir = ".relayforge/worker-tokens"
	}

	return Config{
		AppEnv:                      appEnv,
		DatabaseURL:                 databaseURL,
		DatabaseURLExplicit:         databaseURLExplicit,
		DatabaseConnectTimeout:      10 * time.Second,
		HTTPAddr:                    httpAddr,
		SessionTTL:                  sessionTTL,
		APIKeyPepper:                apiKeyPepper,
		WorkerTokenPepper:           workerTokenPepper,
		RateLimitAuthPerMinute:      rateLimitAuthPerMinute,
		RateLimitAPIPerMinute:       rateLimitAPIPerMinute,
		RateLimitJobSubmitPerMinute: rateLimitJobSubmitPerMinute,

		RegistrationMode:          registrationMode,
		RegistrationModeExplicit:  registrationModeExplicit,
		BootstrapAdminEmail:       strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))),
		InviteTTL:                 inviteTTL,
		TrustProxyHeadersExplicit: trustProxyHeadersExplicit,

		MaxProjectsPerUser:             maxProjectsPerUser,
		MaxWorkerPoolsPerProject:       maxWorkerPoolsPerProject,
		MaxActivePoolsPerProject:       maxActivePoolsPerProject,
		MaxWorkerConcurrencyPerProject: maxWorkerConcurrencyPerProject,
		MaxQueuedJobsPerProject:        maxQueuedJobsPerProject,

		MetricsAddr:    metricsAddr,
		MetricsEnabled: metricsEnabled,

		APISchedulerInterval:   apiSchedulerInterval,
		MaxSchedulesPerProject: maxSchedulesPerProject,

		WorkerAllowPrivateEgress: workerAllowPrivateEgress,
		ActionHTTPTimeout:        actionHTTPTimeout,

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     smtpPort,
		SMTPUsername: os.Getenv("SMTP_USERNAME"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),
		SMTPTLS:      smtpTLS,

		SlackWebhookURL:   os.Getenv("SLACK_WEBHOOK_URL"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		TelegramBotToken:  os.Getenv("TELEGRAM_BOT_TOKEN"),

		TwilioAccountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioAuthToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioFromNumber: os.Getenv("TWILIO_FROM_NUMBER"),

		APIBaseURLExplicit:      apiBaseURLExplicit,
		WorkerMode:              workerMode,
		WorkerProjectID:         os.Getenv("WORKER_PROJECT_ID"),
		WorkerToken:             os.Getenv("WORKER_TOKEN"),
		WorkerTokenFile:         workerTokenFile,
		WorkerEnrollmentToken:   os.Getenv("WORKER_ENROLLMENT_TOKEN"),
		WorkerEnrollmentTTL:     workerEnrollmentTTL,
		APIBaseURL:              apiBaseURL,
		WorkerName:              workerName,
		WorkerConcurrency:       workerConcurrency,
		WorkerPollInterval:      workerPollInterval,
		WorkerJobTimeout:        workerJobTimeout,
		WorkerShutdownTimeout:   workerShutdownTimeout,
		WorkerHeartbeatInterval: workerHeartbeatInterval,
		WorkerCleanupInterval:   workerCleanupInterval,
		RetryBaseDelay:          retryBaseDelay,
		RetryMaxDelay:           retryMaxDelay,
		WorkerLeaseGrace:        workerLeaseGrace,
		WorkerEgressAllowlist:   workerEgressAllowlist,
		WorkerSecretsFile:       os.Getenv("WORKER_SECRETS_FILE"),

		TokenRotationGrace:     tokenRotationGrace,
		APIOutboxInterval:      apiOutboxInterval,
		APICleanupInterval:     apiCleanupInterval,
		APIReaperInterval:      apiReaperInterval,
		APIWorkerSweepInterval: apiWorkerSweepInterval,
		WorkerStaleAfter:       workerStaleAfter,
		TrustProxyHeaders:      trustProxyHeaders,
		WorkerMTLSHeader:       os.Getenv("WORKER_MTLS_HEADER"),

		SupervisorPollInterval: supervisorPollInterval,
		SupervisorWorkerBinary: supervisorWorkerBinary,
		SupervisorTokenDir:     supervisorTokenDir,
	}, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func envNonNegativeInt(name string, fallback int) (int, error) {
	parsed, err := envInt(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return parsed, nil
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func defaultWorkerName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "relayforge-worker"
	}
	return "relayforge-worker-" + hostname
}

// ValidateAPI enforces API-only production requirements. Workers and the
// supervisor do not need API key peppers or registration settings.
func (c Config) ValidateAPI() error {
	if c.AppEnv != "production" {
		return nil
	}
	if c.APIKeyPepper == "" || c.APIKeyPepper == "relayforge-development-pepper" {
		return fmt.Errorf("API_KEY_PEPPER is required when APP_ENV=production")
	}
	if len(c.APIKeyPepper) < 32 {
		return fmt.Errorf("API_KEY_PEPPER must be at least 32 characters when APP_ENV=production")
	}
	if c.WorkerTokenPepper == "" {
		return fmt.Errorf("WORKER_TOKEN_PEPPER is required when APP_ENV=production")
	}
	if len(c.WorkerTokenPepper) < 32 {
		return fmt.Errorf("WORKER_TOKEN_PEPPER must be at least 32 characters when APP_ENV=production")
	}
	if c.WorkerTokenPepper == c.APIKeyPepper {
		return fmt.Errorf("WORKER_TOKEN_PEPPER must differ from API_KEY_PEPPER when APP_ENV=production")
	}
	if !c.RegistrationModeExplicit {
		return fmt.Errorf("REGISTRATION_MODE is required when APP_ENV=production")
	}
	if c.RegistrationMode == "open" {
		return fmt.Errorf("REGISTRATION_MODE=open is not allowed when APP_ENV=production")
	}
	if !c.TrustProxyHeadersExplicit {
		return fmt.Errorf("TRUST_PROXY_HEADERS must be set explicitly when APP_ENV=production")
	}
	return nil
}

// ValidateWorker rejects unsafe worker/supervisor combinations. Remote workers
// must know the API URL, and production deployments must set it explicitly.
func (c Config) ValidateWorker() error {
	if c.AppEnv == "production" && c.WorkerMode == "remote" && !c.APIBaseURLExplicit {
		return fmt.Errorf("RELAYFORGE_API_URL is required for remote workers when APP_ENV=production")
	}
	return nil
}

// LogArgs returns non-secret configuration suitable for a startup log line.
func (c Config) LogArgs() []any {
	return []any{
		"app_env", c.AppEnv,
		"http_addr", c.HTTPAddr,
		"registration_mode", c.RegistrationMode,
		"trust_proxy_headers", c.TrustProxyHeaders,
		"worker_mode", c.WorkerMode,
		"metrics_enabled", c.MetricsEnabled,
		"metrics_addr", c.MetricsAddr,
		"max_projects_per_user", c.MaxProjectsPerUser,
		"max_worker_pools_per_project", c.MaxWorkerPoolsPerProject,
		"max_active_pools_per_project", c.MaxActivePoolsPerProject,
		"max_worker_concurrency_per_project", c.MaxWorkerConcurrencyPerProject,
		"max_queued_jobs_per_project", c.MaxQueuedJobsPerProject,
		"max_schedules_per_project", c.MaxSchedulesPerProject,
		"api_scheduler_interval", c.APISchedulerInterval,
		"action_http_timeout", c.ActionHTTPTimeout,
		"allow_private_egress", c.WorkerAllowPrivateEgress,
	}
}
