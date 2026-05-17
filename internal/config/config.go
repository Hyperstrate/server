package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultPort             = "8090"
	DefaultDatabaseDSN      = "file:hyperstrate-dev.db?cache=shared&_fk=1"
	DefaultFrontendURL      = "http://localhost:8080"
	DefaultOllamaBaseURL    = "http://localhost:11434"
	DefaultCacheRedisAddr   = "localhost:6379"
	DefaultCacheRedisPrefix = "hs"
)

type Config struct {
	AppEnv      string
	Port        string
	DatabaseDSN string
	JWTSecret   string

	// APIPublicURL is the externally reachable base URL of the API. When set,
	// generated Swagger docs use it for host, scheme, and base path.
	// Example: API_PUBLIC_URL=https://api.hyperstrate.example
	APIPublicURL string

	// AdminEmail, when set, guarantees that the user with this email always has
	// the admin role regardless of signup order.
	// Example: ADMIN_EMAIL=alice@example.com
	AdminEmail string

	// FrontendURL is the base URL of the frontend app. After a successful OIDC
	// callback the server redirects to {FrontendURL}/auth/callback?token=...
	// Example: FRONTEND_URL=http://localhost:8080
	FrontendURL string

	// OIDCJWKSUrl is the JWKS endpoint of the OIDC provider used to validate
	// inbound access tokens at POST /auth/oidc/exchange.
	// Example: OIDC_JWKS_URL=https://<project>.supabase.co/auth/v1/.well-known/jwks.json
	OIDCJWKSUrl string

	// OIDCProviders is the list of OAuth provider names the OIDC provider has
	// enabled (e.g. "google,github"). Controls which login buttons appear on the
	// frontend login page. No credentials needed here — those live in the OIDC
	// provider's dashboard.
	// Example: OIDC_PROVIDERS=google,github
	OIDCProviders []string

	// LogRetentionDays is how many days inference_logs and audit_logs are kept.
	// A nightly job purges older records. Default: 90.
	LogRetentionDays int

	// OllamaBaseURL is the default base URL used by /ai/discover when the caller
	// does not pass a baseUrl query parameter.
	OllamaBaseURL string

	// SQSQueueURL enables the SQS async job dispatcher when set.
	SQSQueueURL string

	// CacheBackend selects the response-cache strategy: "memory" (default) or "redis".
	CacheBackend string

	// CacheRedisAddr and CacheRedisPrefix configure the Redis response cache.
	CacheRedisAddr   string
	CacheRedisPrefix string

	// RateLimitBackend selects the rate-limiter store: "memory" (default) or "redis".
	RateLimitBackend string

	// HealthCheckIntervalSecs is the interval between provider health probes. Default: 120.
	HealthCheckIntervalSecs int
}

func New() Config {
	appEnv := getEnv("APP_ENV", "development")
	// Load .env then .env.local (overrides) only outside production.
	// In production, process environment must be the only config source.
	if appEnv != "production" {
		_ = godotenv.Load()
		_ = godotenv.Overload(".env.local")
		appEnv = getEnv("APP_ENV", appEnv)
	}

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		if appEnv == "production" {
			fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET must be set in production")
			os.Exit(1)
		}
		var err error
		jwtSecret, err = generateDevelopmentJWTSecret()
		if err != nil {
			fmt.Fprintln(os.Stderr, "FATAL: could not generate development JWT_SECRET:", err)
			os.Exit(1)
		}
		slog.Warn("JWT_SECRET not set; generated an ephemeral development secret — sessions will reset on restart")
	}
	return Config{
		AppEnv:                  appEnv,
		Port:                    getEnv("PORT", DefaultPort),
		DatabaseDSN:             getEnv("DATABASE_DSN", DefaultDatabaseDSN),
		JWTSecret:               jwtSecret,
		APIPublicURL:            getEnv("API_PUBLIC_URL", ""),
		AdminEmail:              getEnv("ADMIN_EMAIL", ""),
		FrontendURL:             getEnv("FRONTEND_URL", DefaultFrontendURL),
		OIDCJWKSUrl:             getEnv("OIDC_JWKS_URL", ""),
		OIDCProviders:           parseCommaSeparated(os.Getenv("OIDC_PROVIDERS")),
		LogRetentionDays:        getEnvInt("LOG_RETENTION_DAYS", 90),
		OllamaBaseURL:           getEnv("OLLAMA_BASE_URL", DefaultOllamaBaseURL),
		SQSQueueURL:             getEnv("SQS_QUEUE_URL", ""),
		CacheBackend:            getEnv("CACHE_BACKEND", "memory"),
		CacheRedisAddr:          getEnv("CACHE_REDIS_ADDR", DefaultCacheRedisAddr),
		CacheRedisPrefix:        getEnv("CACHE_REDIS_PREFIX", DefaultCacheRedisPrefix),
		RateLimitBackend:        getEnv("RATE_LIMIT_BACKEND", "memory"),
		HealthCheckIntervalSecs: getEnvInt("HEALTH_CHECK_INTERVAL_SECS", 120),
	}
}

func generateDevelopmentJWTSecret() (string, error) {
	secret := make([]byte, 64)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(secret), nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func parseCommaSeparated(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
