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

	DatabaseURLExplicit bool

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

	apiKeyPepper := os.Getenv("API_KEY_PEPPER")
	if apiKeyPepper == "" {
		if appEnv == "production" {
			return Config{}, fmt.Errorf("API_KEY_PEPPER is required when APP_ENV=production")
		}
		apiKeyPepper = "relayforge-development-pepper"
	}
	if len(apiKeyPepper) < 16 {
		return Config{}, fmt.Errorf("API_KEY_PEPPER must be at least 16 characters")
	}

	workerTokenPepper := os.Getenv("WORKER_TOKEN_PEPPER")
	if workerTokenPepper == "" {
		workerTokenPepper = apiKeyPepper
	}
	if len(workerTokenPepper) < 16 {
		return Config{}, fmt.Errorf("WORKER_TOKEN_PEPPER must be at least 16 characters")
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
	if value := strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS")); value != "" {
		trustProxyHeaders, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse TRUST_PROXY_HEADERS: %w", err)
		}
	}
	workerEgressAllowlist := splitList(os.Getenv("WORKER_EGRESS_ALLOWLIST"))

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
