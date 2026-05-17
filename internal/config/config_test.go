package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDoesNotLoadDotenvFilesInProduction(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=1111\nDATABASE_DSN=file:dotenv.db\nJWT_SECRET=dotenv-secret\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("APP_ENV=development\nPORT=2222\nDATABASE_DSN=file:local.db\nJWT_SECRET=local-secret\n"), 0o600); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "real-production-secret")
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_DSN", "")

	cfg := New()

	if cfg.AppEnv != "production" {
		t.Fatalf("AppEnv = %q, want production", cfg.AppEnv)
	}
	if cfg.JWTSecret != "real-production-secret" {
		t.Fatalf("JWTSecret was loaded from dotenv file")
	}
	if cfg.Port != "8090" {
		t.Fatalf("Port = %q, want default 8090", cfg.Port)
	}
	if cfg.DatabaseDSN != "file:hyperstrate-dev.db?cache=shared&_fk=1" {
		t.Fatalf("DatabaseDSN = %q, want default sqlite DSN", cfg.DatabaseDSN)
	}
}

func TestNewGeneratesDevelopmentJWTSecretWhenUnset(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	t.Setenv("APP_ENV", "development")
	t.Setenv("JWT_SECRET", "")

	first := New()
	second := New()

	if first.JWTSecret == "" {
		t.Fatalf("first generated JWTSecret is empty")
	}
	if second.JWTSecret == "" {
		t.Fatalf("second generated JWTSecret is empty")
	}
	if first.JWTSecret == second.JWTSecret {
		t.Fatalf("development JWTSecret should be generated per process config load, got identical value")
	}
}

func TestNewReadsRuntimeURLAndBackendSettingsFromEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("API_PUBLIC_URL", "https://api.example.com/base")
	t.Setenv("OLLAMA_BASE_URL", "http://ollama.internal:11434")
	t.Setenv("SQS_QUEUE_URL", "https://sqs.example.com/queue")
	t.Setenv("CACHE_BACKEND", "redis")
	t.Setenv("CACHE_REDIS_ADDR", "redis.internal:6379")
	t.Setenv("CACHE_REDIS_PREFIX", "custom")

	cfg := New()

	if cfg.APIPublicURL != "https://api.example.com/base" {
		t.Fatalf("APIPublicURL = %q", cfg.APIPublicURL)
	}
	if cfg.OllamaBaseURL != "http://ollama.internal:11434" {
		t.Fatalf("OllamaBaseURL = %q", cfg.OllamaBaseURL)
	}
	if cfg.SQSQueueURL != "https://sqs.example.com/queue" {
		t.Fatalf("SQSQueueURL = %q", cfg.SQSQueueURL)
	}
	if cfg.CacheBackend != "redis" {
		t.Fatalf("CacheBackend = %q", cfg.CacheBackend)
	}
	if cfg.CacheRedisAddr != "redis.internal:6379" {
		t.Fatalf("CacheRedisAddr = %q", cfg.CacheRedisAddr)
	}
	if cfg.CacheRedisPrefix != "custom" {
		t.Fatalf("CacheRedisPrefix = %q", cfg.CacheRedisPrefix)
	}
}
