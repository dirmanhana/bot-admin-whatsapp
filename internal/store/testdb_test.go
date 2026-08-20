package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// testDSN mengembalikan DSN PostgreSQL untuk test (TEST_DATABASE_URL atau
// default lokal).
func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://dirman@127.0.0.1:5433/bot_admin_whatsapp_test?sslmode=disable"
}

// resetSchema mengosongkan semua data (skema & migrasi tetap) agar tiap
// test mulai dari kondisi bersih. Tidak memakai DROP SCHEMA karena cache
// skema di pgx bisa membuat koneksi lain kehilangan search_path.
func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DO $$ DECLARE r record; BEGIN
		FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations' LOOP
			EXECUTE format('TRUNCATE TABLE public.%I CASCADE', r.tablename);
		END LOOP;
	END $$`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// Reset sequence tenant agar id tenant mulai dari 1 lagi di tiap test.
	_, _ = db.Exec(`SELECT setval('tenants_id_seq', 1, false)`)
}

// openTestDB menghubungkan ke PostgreSQL, mereset skema, menjalankan migrasi,
// lalu men-seed tenant 1. Test di-skip bila Postgres tidak tersedia.
func openTestDB(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db, err := Open(context.Background(), testDSN())
	if err != nil {
		t.Skipf("postgres tidak tersedia, skip: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	resetSchema(t, db)

	st := New(db)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.SeedTenant1(context.Background(), "admin@example.com", "hash1"); err != nil {
		t.Fatalf("seed tenant1: %v", err)
	}
	return db, st
}
