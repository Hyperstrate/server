package db

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDialectorForDSNSelectsSupportedDialects(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		dialect string
	}{
		{name: "default sqlite file", dsn: "file:hyperstrate-dev.db?cache=shared&_fk=1", dialect: "sqlite"},
		{name: "sqlite memory", dsn: ":memory:", dialect: "sqlite"},
		{name: "postgres url", dsn: "postgres://user:pass@localhost:5432/hyperstrate?sslmode=disable", dialect: "postgres"},
		{name: "postgresql url", dsn: "postgresql://user:pass@localhost:5432/hyperstrate?sslmode=disable", dialect: "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialector, err := dialectorForDSN(tt.dsn)
			if err != nil {
				t.Fatalf("dialectorForDSN returned error: %v", err)
			}
			if got := dialector.Name(); got != tt.dialect {
				t.Fatalf("dialector name = %q, want %q", got, tt.dialect)
			}
		})
	}
}

func TestDialectorForDSNRejectsUnsupportedProductionDialects(t *testing.T) {
	_, err := dialectorForDSN("mysql://user:pass@tcp(localhost:3306)/hyperstrate")
	if err == nil {
		t.Fatal("dialectorForDSN(mysql) returned nil error, want unsupported dialect error")
	}
}

func TestMigrationDirForDialect(t *testing.T) {
	tests := []struct {
		dialect string
		dir     string
	}{
		{dialect: "sqlite", dir: "migrations/sqlite"},
		{dialect: "postgres", dir: "migrations/postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.dialect, func(t *testing.T) {
			dir, err := migrationDirForDialect(tt.dialect)
			if err != nil {
				t.Fatalf("migrationDirForDialect returned error: %v", err)
			}
			if dir != tt.dir {
				t.Fatalf("dir = %q, want %q", dir, tt.dir)
			}
		})
	}
}

func TestEmbeddedMigrationHistoryKeepsBaselineFiles(t *testing.T) {
	tests := []struct {
		dir  string
		want []string
	}{
		{
			dir: "migrations/sqlite",
			want: []string{
				"20260518000001_auth.sql",
				"20260518000004_routers.sql",
				"20260518000006_observability.sql",
			},
		},
		{
			dir: "migrations/postgres",
			want: []string{
				"20260518000001_auth.sql",
				"20260518000004_routers.sql",
				"20260518000006_observability.sql",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			dir, err := newEmbedDir(migrationsFS, tt.dir)
			if err != nil {
				t.Fatalf("newEmbedDir: %v", err)
			}
			files, err := dir.Files()
			if err != nil {
				t.Fatalf("Files: %v", err)
			}
			seen := map[string]bool{}
			for _, file := range files {
				seen[file.Name()] = true
			}
			for _, name := range tt.want {
				if !seen[name] {
					t.Fatalf("embedded migration %s missing from %s", name, tt.dir)
				}
			}
		})
	}
}

func TestMigrateAppliesEmbeddedSQLiteMigrationsFromScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hyperstrate.db")
	database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	for _, table := range []string{"mcp_servers", "router_evaluations", "agent_session_events", "compression_events"} {
		if !database.Migrator().HasTable(table) {
			t.Fatalf("expected migrated table %q to exist", table)
		}
	}
	for _, column := range []struct {
		table  string
		column string
	}{
		{table: "mcp_servers", column: "extra_headers"},
		{table: "conversations", column: "modified_at"},
		{table: "inference_logs", column: "ttft_ms"},
		{table: "auth_virtual_keys", column: "rate_limit_rps"},
	} {
		if !database.Migrator().HasColumn(column.table, column.column) {
			t.Fatalf("expected migrated column %s.%s to exist", column.table, column.column)
		}
	}
}
