package db

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"hyperstrate/server/internal/config"

	"ariga.io/atlas/sql/migrate"
	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlassqlite "ariga.io/atlas/sql/sqlite"
	gpostgres "gorm.io/driver/postgres"
	gsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed migrations/sqlite migrations/postgres
var migrationsFS embed.FS

func New(cfg config.Config) (*gorm.DB, error) {
	dialector, err := dialectorForDSN(cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		// Warn level: only log slow queries and real errors, not ErrRecordNotFound.
		// "record not found" is a normal control-flow result (e.g. auth fallback),
		// not an error worth logging on every request.
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	// SQLite only allows one writer at a time. Without WAL mode and a single
	// open connection, concurrent requests cause "database is locked" errors.
	if db.Dialector.Name() == "sqlite" {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get underlying sql.DB: %w", err)
		}
		sqlDB.SetMaxOpenConns(1)
		if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			return nil, fmt.Errorf("enable WAL mode: %w", err)
		}
	}

	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}

func dialectorForDSN(dsn string) (gorm.Dialector, error) {
	normalized := strings.ToLower(strings.TrimSpace(dsn))
	switch {
	case strings.HasPrefix(normalized, "postgres://"), strings.HasPrefix(normalized, "postgresql://"):
		return gpostgres.Open(dsn), nil
	case strings.HasPrefix(normalized, "mysql://"), strings.Contains(normalized, "@tcp("):
		return nil, fmt.Errorf("unsupported database dialect in DATABASE_DSN: mysql")
	default:
		return gsqlite.Open(dsn), nil
	}
}

// Migrate applies any pending Atlas versioned migrations embedded in the binary.
// Applied migrations are tracked in atlas_schema_revisions so they are never re-run.
func Migrate(database *gorm.DB) error {
	dialect := database.Dialector.Name()
	slog.Info("applying pending migrations", "dialect", dialect)

	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("get underlying sql.DB: %w", err)
	}

	driver, err := atlasDriverForDialect(dialect, sqlDB)
	if err != nil {
		return fmt.Errorf("open atlas %s driver: %w", dialect, err)
	}

	migrationsDir, err := migrationDirForDialect(dialect)
	if err != nil {
		return err
	}

	dir, err := newEmbedDir(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	rrw, err := newRevisionRW(sqlDB, dialect)
	if err != nil {
		return fmt.Errorf("init revision tracking: %w", err)
	}

	ex, err := migrate.NewExecutor(driver, dir, rrw, migrate.WithAllowDirty(true))
	if err != nil {
		return fmt.Errorf("create migration executor: %w", err)
	}

	if err := ex.ExecuteN(context.Background(), 0); err != nil && err != migrate.ErrNoPendingFiles {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

func atlasDriverForDialect(dialect string, sqlDB *sql.DB) (migrate.Driver, error) {
	switch dialect {
	case "sqlite":
		return atlassqlite.Open(sqlDB)
	case "postgres":
		return atlaspostgres.Open(sqlDB)
	default:
		return nil, fmt.Errorf("unsupported database dialect: %s", dialect)
	}
}

func migrationDirForDialect(dialect string) (string, error) {
	switch dialect {
	case "sqlite":
		return "migrations/sqlite", nil
	case "postgres":
		return "migrations/postgres", nil
	default:
		return "", fmt.Errorf("unsupported database dialect: %s", dialect)
	}
}

// ── embed.FS → migrate.Dir adapter ───────────────────────────────────────────

// embedDir wraps an embed.FS sub-tree and satisfies migrate.Dir.
type embedDir struct {
	files   []migrate.File
	hf      migrate.HashFile
	sumData []byte // raw atlas.sum content served to the Atlas validator
}

func newEmbedDir(fsys embed.FS, root string) (*embedDir, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}

	var files []migrate.File
	for _, e := range entries {
		if e.IsDir() || e.Name() == "atlas.sum" {
			continue
		}
		data, err := fs.ReadFile(fsys, root+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, migrate.NewLocalFile(e.Name(), data))
	}

	hf, err := migrate.NewHashFile(files)
	if err != nil {
		return nil, err
	}

	// Use committed atlas.sum when present; otherwise generate one from file hashes.
	// WithAllowDirty skips checksum validation at runtime, so a generated sum is fine.
	sumData, err := fs.ReadFile(fsys, root+"/atlas.sum")
	if errors.Is(err, fs.ErrNotExist) {
		sumData, err = hf.MarshalText()
	}
	if err != nil {
		return nil, fmt.Errorf("atlas.sum: %w", err)
	}

	return &embedDir{files: files, hf: hf, sumData: sumData}, nil
}

// Open implements fs.FS (required by migrate.Dir).
// The Atlas executor calls Open("atlas.sum") to validate the directory checksum.
func (d *embedDir) Open(name string) (fs.File, error) {
	if name == "atlas.sum" {
		return &embedFile{data: d.sumData, name: name}, nil
	}
	for _, f := range d.files {
		if f.Name() == name {
			return &embedFile{data: f.Bytes(), name: f.Name()}, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// WriteFile is a no-op for a read-only embedded dir.
func (d *embedDir) WriteFile(_ string, _ []byte) error {
	return fmt.Errorf("embedded migration dir is read-only")
}

// Files returns the ordered list of migration files.
func (d *embedDir) Files() ([]migrate.File, error) { return d.files, nil }

// Checksum returns the precomputed HashFile.
func (d *embedDir) Checksum() (migrate.HashFile, error) { return d.hf, nil }

// embedFile is a minimal fs.File backed by a byte slice.
type embedFile struct {
	data   []byte
	name   string
	offset int
}

func (f *embedFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	if f.offset >= len(f.data) {
		return n, io.EOF
	}
	return n, nil
}
func (f *embedFile) Close() error               { return nil }
func (f *embedFile) Stat() (fs.FileInfo, error) { return nil, fmt.Errorf("stat not supported") }

// ── RevisionReadWriter ───────────────────────────────────────────────────────

type revisionRW struct {
	db      *sql.DB
	dialect string
}

func newRevisionRW(db *sql.DB, dialect string) (*revisionRW, error) {
	createSQL := `CREATE TABLE IF NOT EXISTS atlas_schema_revisions (
		version          TEXT    NOT NULL PRIMARY KEY,
		description      TEXT    NOT NULL DEFAULT '',
		type             INTEGER NOT NULL DEFAULT 0,
		applied          INTEGER NOT NULL DEFAULT 0,
		total            INTEGER NOT NULL DEFAULT 0,
		executed_at      DATETIME NOT NULL,
		execution_time   INTEGER NOT NULL DEFAULT 0,
		error            TEXT    NOT NULL DEFAULT '',
		error_stmt       TEXT    NOT NULL DEFAULT '',
		hash             TEXT    NOT NULL DEFAULT '',
		partial_hashes   TEXT    NOT NULL DEFAULT '[]',
		operator_version TEXT    NOT NULL DEFAULT ''
	)`
	if dialect == "postgres" {
		createSQL = `CREATE TABLE IF NOT EXISTS atlas_schema_revisions (
		version          TEXT        NOT NULL PRIMARY KEY,
		description      TEXT        NOT NULL DEFAULT '',
		type             INTEGER     NOT NULL DEFAULT 0,
		applied          INTEGER     NOT NULL DEFAULT 0,
		total            INTEGER     NOT NULL DEFAULT 0,
		executed_at      TIMESTAMPTZ NOT NULL,
		execution_time   BIGINT      NOT NULL DEFAULT 0,
		error            TEXT        NOT NULL DEFAULT '',
		error_stmt       TEXT        NOT NULL DEFAULT '',
		hash             TEXT        NOT NULL DEFAULT '',
		partial_hashes   TEXT        NOT NULL DEFAULT '[]',
		operator_version TEXT        NOT NULL DEFAULT ''
	)`
	}
	_, err := db.Exec(createSQL)
	if err != nil {
		return nil, err
	}
	return &revisionRW{db: db, dialect: dialect}, nil
}

func (r *revisionRW) Ident() *migrate.TableIdent {
	return &migrate.TableIdent{Name: "atlas_schema_revisions"}
}

func (r *revisionRW) ReadRevisions(ctx context.Context) ([]*migrate.Revision, error) {
	rows, err := r.db.QueryContext(ctx, revisionSelectSQL+` ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revs []*migrate.Revision
	for rows.Next() {
		rev, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		revs = append(revs, rev)
	}
	return revs, rows.Err()
}

func (r *revisionRW) ReadRevision(ctx context.Context, version string) (*migrate.Revision, error) {
	row := r.db.QueryRowContext(ctx, revisionSelectSQL+` WHERE version = `+r.placeholder(1), version)
	rev, err := scanRevision(row)
	if err == sql.ErrNoRows {
		return nil, migrate.ErrRevisionNotExist
	}
	return rev, err
}

func (r *revisionRW) WriteRevision(ctx context.Context, rev *migrate.Revision) error {
	ph, err := json.Marshal(rev.PartialHashes)
	if err != nil {
		return err
	}
	query := `INSERT OR REPLACE INTO atlas_schema_revisions
			 (version, description, type, applied, total, executed_at, execution_time,
			  error, error_stmt, hash, partial_hashes, operator_version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if r.dialect == "postgres" {
		query = `INSERT INTO atlas_schema_revisions
			 (version, description, type, applied, total, executed_at, execution_time,
			  error, error_stmt, hash, partial_hashes, operator_version)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (version) DO UPDATE SET
			  description = EXCLUDED.description,
			  type = EXCLUDED.type,
			  applied = EXCLUDED.applied,
			  total = EXCLUDED.total,
			  executed_at = EXCLUDED.executed_at,
			  execution_time = EXCLUDED.execution_time,
			  error = EXCLUDED.error,
			  error_stmt = EXCLUDED.error_stmt,
			  hash = EXCLUDED.hash,
			  partial_hashes = EXCLUDED.partial_hashes,
			  operator_version = EXCLUDED.operator_version`
	}
	_, err = r.db.ExecContext(ctx,
		query,
		rev.Version, rev.Description, int(rev.Type),
		rev.Applied, rev.Total, rev.ExecutedAt, int64(rev.ExecutionTime),
		rev.Error, rev.ErrorStmt, rev.Hash, string(ph), rev.OperatorVersion,
	)
	return err
}

func (r *revisionRW) DeleteRevision(ctx context.Context, version string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM atlas_schema_revisions WHERE version = `+r.placeholder(1), version)
	return err
}

func (r *revisionRW) placeholder(n int) string {
	if r.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

const revisionSelectSQL = `SELECT version, description, type, applied, total,
	executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version
	FROM atlas_schema_revisions`

type scanner interface{ Scan(dest ...any) error }

func scanRevision(row scanner) (*migrate.Revision, error) {
	var (
		rev    migrate.Revision
		execNs int64
		phJSON string
	)
	err := row.Scan(
		&rev.Version, &rev.Description, &rev.Type,
		&rev.Applied, &rev.Total, &rev.ExecutedAt, &execNs,
		&rev.Error, &rev.ErrorStmt, &rev.Hash, &phJSON, &rev.OperatorVersion,
	)
	if err != nil {
		return nil, err
	}
	rev.ExecutionTime = time.Duration(execNs)
	_ = json.Unmarshal([]byte(phJSON), &rev.PartialHashes)
	return &rev, nil
}
