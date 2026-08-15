package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/postgres/*.sql migrations/sqlite/*.sql
var migrationsFS embed.FS

// Open connects to the database. dialect is "postgres" (default, DSN gaya
// postgres://) atau "sqlite" (path file .db lokal, murni Go, tanpa CGO —
// cocok untuk build Windows .exe).
func Open(ctx context.Context, dialect, dsn string) (*sql.DB, error) {
	if dialect == "sqlite" {
		if dsn == "" || strings.HasPrefix(dsn, "postgres://") {
			dsn = "bot_admin_whatsapp.db"
		}
		if !strings.HasPrefix(dsn, "file:") {
			dsn = "file:" + dsn
		}
		// Pragmas penting: WAL untuk konkurensi, foreign_keys untuk ON DELETE
		// CASCADE, busy_timeout agar tidak langsung SQLITE_BUSY.
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_time_format=sqlite"
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		db.SetMaxOpenConns(1) // SQLite aman dengan satu koneksi tulis pada satu waktu
		if err := db.PingContext(ctx); err != nil {
			return nil, fmt.Errorf("ping sqlite: %w", err)
		}
		return db, nil
	}

	// default: postgres
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

// Migrate menjalankan migrasi SQL yang belum diterapkan (per dialect).
func (s *Store) Migrate(ctx context.Context) error {
	ddl := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`
	if s.dialect == "sqlite" {
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`
	}
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	dir := "migrations/" + s.dialect
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return err
		}
		if err := s.applyMigration(ctx, name, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, name, sqlText string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, s.q(sqlText)); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO schema_migrations (version) VALUES ($1)`), name); err != nil {
		return err
	}
	return tx.Commit()
}
