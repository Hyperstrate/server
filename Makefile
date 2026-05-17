build-ApiFunction:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap ./cmd/lambda

build-WorkerFunction:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o worker-bootstrap ./cmd/worker

run:
	go run ./cmd/api


test:
	gotestsum --format testname ./...

test-verbose:
	gotestsum --format standard-verbose ./...

swagger:
	swag init -g ./cmd/api/main.go -o ./docs --parseDependency --parseInternal
	node ./scripts/build-openapi3.mjs

# ── Atlas migrations ──────────────────────────────────────────────────────────
# Generate a new versioned migration file from the current GORM models.
# Usage: make migrate-diff name=add_foo_column
migrate-diff:
	atlas migrate diff --env local $(name)

migrate-diff-prod:
	atlas migrate diff --env production $(name)

# Rehash after manually editing migration files.
migrate-hash:
	atlas migrate hash --env local

migrate-hash-prod:
	atlas migrate hash --env production

# Apply pending migrations — local SQLite dev DB.
migrate-apply:
	atlas migrate apply --env local

# Apply pending migrations — production DB (requires DATABASE_URL + DATABASE_DEV_URL).
# Usage: DATABASE_URL=postgres://... DATABASE_DEV_URL=postgres://... make migrate-apply-prod
migrate-apply-prod:
	atlas migrate apply --env production

# Show migration status — local dev DB.
migrate-status:
	atlas migrate status --env local

# Show migration status — production DB (requires DATABASE_URL).
# Usage: DATABASE_URL=postgres://... make migrate-status-prod
migrate-status-prod:
	atlas migrate status --env production

# ── Dev seeds ─────────────────────────────────────────────────────────────────
# Seed inference_logs with 200 synthetic rows spread over 30 days (idempotent).
seed-inference-logs:
	sqlite3 hyperstrate-dev.db < internal/db/seeds/inference_logs.sql

seed-agent-sessions:
	sqlite3 hyperstrate-dev.db < internal/db/seeds/agent_sessions.sql
