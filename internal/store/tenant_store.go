package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ErrEmailTaken adalah error saat email tenant sudah dipakai.
var ErrEmailTaken = errors.New("email sudah terdaftar")

const (
	defaultWelcome = "Halo! Selamat datang di {store_name}. Ketik *menu* untuk melihat katalog produk kami."
	defaultCatalog = "Berikut katalog {store_name}:\n\n{products}\n\nKetik nomor produk untuk memesan, atau ketik *menu* untuk melihat ulang."
)

// CreateTenant membuat tenant baru beserta pengaturan awal toko.
func (s *Store) CreateTenant(ctx context.Context, email, passwordHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	err = tx.QueryRowContext(ctx, s.q(`
		INSERT INTO tenants (email, password_hash, status, session_epoch, webhook_secret)
		VALUES ($1, $2, 'active', 0, $3)
		RETURNING id`), email, passwordHash, randomSecret()).Scan(&id)
	if err != nil {
		return 0, err
	}

	// Seed pengaturan default toko baru (welcome & intro katalog).
	for _, kv := range []struct{ k, v string }{
		{"store_name", "Toko Saya"},
		{"welcome_message", defaultWelcome},
		{"catalog_intro", defaultCatalog},
	} {
		if _, err := tx.ExecContext(ctx, s.q(`
			INSERT INTO settings (tenant_id, key, value) VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value`), id, kv.k, kv.v); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) GetTenantByEmail(ctx context.Context, email string) (*Tenant, error) {
	var t Tenant
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, email, password_hash, status, session_epoch, created_at, updated_at
		FROM tenants WHERE email = $1`), email).
		Scan(&t.ID, &t.Email, &t.PasswordHash, &t.Status, &t.SessionEpoch, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) GetTenant(ctx context.Context, id int64) (*Tenant, error) {
	var t Tenant
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, email, password_hash, status, session_epoch, created_at, updated_at
		FROM tenants WHERE id = $1`), id).
		Scan(&t.ID, &t.Email, &t.PasswordHash, &t.Status, &t.SessionEpoch, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, email, password_hash, status, session_epoch, created_at, updated_at
		FROM tenants ORDER BY id ASC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Email, &t.PasswordHash, &t.Status, &t.SessionEpoch, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTenantPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE tenants SET password_hash = $1, updated_at = $2 WHERE id = $3`),
		hash, time.Now(), id)
	return err
}

func (s *Store) UpdateTenantSessionEpoch(ctx context.Context, id int64, epoch int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE tenants SET session_epoch = $1, updated_at = $2 WHERE id = $3`),
		epoch, time.Now(), id)
	return err
}

func (s *Store) BumpTenantSessionEpoch(ctx context.Context, id int64) error {
	t, err := s.GetTenant(ctx, id)
	if err != nil {
		return err
	}
	if t == nil {
		return sql.ErrNoRows
	}
	return s.UpdateTenantSessionEpoch(ctx, id, t.SessionEpoch+1)
}

func (s *Store) SetTenantStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE tenants SET status = $1, updated_at = $2 WHERE id = $3`),
		status, time.Now(), id)
	return err
}

func (s *Store) TenantWebhookSecret(ctx context.Context, id int64) (string, error) {
	var secret string
	err := s.db.QueryRowContext(ctx, s.q(`SELECT webhook_secret FROM tenants WHERE id = $1`), id).Scan(&secret)
	return secret, err
}

// TenantIDByDeviceID mencari tenant pemilik device gowa (dipakai untuk
// mengarahkan webhook ke tenant yang benar). Mengembalikan 0 bila tidak
// ditemukan. Satu device hanya boleh milik satu tenant (lihat UpsertWAAccount),
// jadi hasilnya deterministik.
func (s *Store) TenantIDByDeviceID(ctx context.Context, deviceID string) (int64, error) {
	if deviceID == "" {
		return 0, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT tenant_id FROM wa_accounts WHERE device_id = $1 ORDER BY id LIMIT 1`), deviceID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListTenantIDs mengembalikan semua ID tenant aktif (dipakai broadcast
// worker agar broadcast tiap toko dikirim dengan konteks tenant-nya).
func (s *Store) ListTenantIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id FROM tenants WHERE status = 'active' ORDER BY id ASC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) RegenerateTenantWebhookSecret(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE tenants SET webhook_secret = $1, updated_at = $2 WHERE id = $3`),
		randomSecret(), time.Now(), id)
	return err
}

// SeedTenant1 membuat tenant pertama (dari DASHBOARD_USER/PASSWORD di .env)
// HANYA saat tabel tenants masih kosong (instalasi baru). Tidak menyala
// ulang tenant yang sudah dihapus; operator cukup daftar ulang lewat
// /admin/register. Sequence id ikut disetel agar tenant berikutnya
// (registrasi) tidak bentrok dengan id 1.
func (s *Store) SeedTenant1(ctx context.Context, email, passwordHash string) error {
	var count int64
	if err := s.db.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM tenants`)).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // sudah ada tenant (mis. tenant 1 dihapus sengaja) — jangan dibangkitkan lagi
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.q(`
		INSERT INTO tenants (id, email, password_hash, status, session_epoch, webhook_secret)
		VALUES (1, $1, $2, 'active', 0, $3)`), email, passwordHash, randomSecret()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`SELECT setval('tenants_id_seq', GREATEST((SELECT COALESCE(MAX(id), 1) FROM tenants), 1))`)); err != nil {
		return err
	}
	return tx.Commit()
}

func randomSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
