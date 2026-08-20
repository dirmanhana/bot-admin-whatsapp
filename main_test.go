package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://dirman@127.0.0.1:5433/bot_admin_whatsapp_test?sslmode=disable"
}

// truncateAll mengosongkan semua tabel (skema & migrasi tetap).
func truncateAll(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DO $$ DECLARE r record; BEGIN
		FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations' LOOP
			EXECUTE format('TRUNCATE TABLE public.%I CASCADE', r.tablename);
		END LOOP;
	END $$`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, _ = db.Exec(`SELECT setval('tenants_id_seq', 1, false)`)
}

// testStore menyiapkan DB Postgres dengan tenant 1 (seed) dan tenant 2 yang
// punya device gowa "dev-2".
func testStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), testDSN())
	if err != nil {
		t.Skipf("postgres tidak tersedia, skip: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	truncateAll(t, db)

	st := store.New(db)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.SeedTenant1(context.Background(), "admin@example.com", "hash"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := st.CreateTenant(context.Background(), "toko2@example.com", "hash2"); err != nil {
		t.Fatalf("create tenant2: %v", err)
	}
	ctx2 := store.WithTenant(context.Background(), 2)
	if _, err := st.UpsertWAAccount(ctx2, "user2", "pass2", "dev-2", true); err != nil {
		t.Fatalf("wa account tenant2: %v", err)
	}
	return st
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookBody(deviceID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"event":     "message",
		"device_id": deviceID,
		"payload": map[string]any{
			"id": "abc", "is_from_me": false, "from": "628111@s.whatsapp.net", "body": "halo",
		},
	})
	return b
}

func TestVerifyHMAC(t *testing.T) {
	body := []byte("hello")
	if !verifyHMAC("secret", body, sign("secret", body)) {
		t.Fatal("harus cocok dengan secret yang sama")
	}
	if verifyHMAC("secret", body, sign("lain", body)) {
		t.Fatal("tidak boleh cocok dengan secret berbeda")
	}
	if verifyHMAC("", body, sign("x", body)) {
		t.Fatal("secret kosong harus ditolak")
	}
}

func TestResolveWebhookTenantGlobalSecret(t *testing.T) {
	st := testStore(t)
	cfg := &config.Config{GowaWebhookSecret: "global-secret"}

	// Device tenant 2 + signature global → tenant 2.
	body := webhookBody("dev-2")
	tid, err := resolveWebhookTenant(body, sign("global-secret", body), st, cfg)
	if err != nil || tid != 2 {
		t.Fatalf("global secret + dev-2: tid=%d err=%v", tid, err)
	}

	// Device tak dikenal + signature global → DITOLAK (fail-closed, tidak
	// boleh jatuh ke tenant 1).
	unknown := webhookBody("dev-tidak-dikenal")
	tid, err = resolveWebhookTenant(unknown, sign("global-secret", unknown), st, cfg)
	if err == nil {
		t.Fatalf("device tak dikenal harus ditolak, dapat tid=%d", tid)
	}

	// Signature global salah → ditolak.
	_, err = resolveWebhookTenant(body, sign("salah", body), st, cfg)
	if err == nil {
		t.Fatal("signature salah harus ditolak")
	}
}

func TestResolveWebhookTenantPerTenantSecret(t *testing.T) {
	st := testStore(t)
	secret2, err := st.TenantWebhookSecret(context.Background(), 2)
	if err != nil || secret2 == "" {
		t.Fatalf("secret tenant2: %q %v", secret2, err)
	}
	cfg := &config.Config{GowaWebhookSecret: "global-secret"}

	body := webhookBody("dev-2")
	// Signature pakai secret tenant 2 (global tidak cocok) → tenant 2.
	tid, err := resolveWebhookTenant(body, sign(secret2, body), st, cfg)
	if err != nil || tid != 2 {
		t.Fatalf("secret tenant2: tid=%d err=%v", tid, err)
	}

	// Signature pakai secret acak → ditolak.
	_, err = resolveWebhookTenant(body, sign("random", body), st, cfg)
	if err == nil {
		t.Fatal("secret acak harus ditolak")
	}

	// Device tak dikenal + signature apa pun (bukan global/tenant) → ditolak.
	unknown := webhookBody("dev-x")
	_, err = resolveWebhookTenant(unknown, sign("random", unknown), st, cfg)
	if err == nil {
		t.Fatal("device tak dikenal dengan secret non-global harus ditolak")
	}
}

func TestResolveWebhookTenantBySignatureJID(t *testing.T) {
	// gowa mengirim device_id berupa JID (bukan UUID device). Identitas tenant
	// harus dikenali dari signature-nya, bukan dari device_id.
	st := testStore(t)
	secret1, _ := st.TenantWebhookSecret(context.Background(), 1)
	secret2, _ := st.TenantWebhookSecret(context.Background(), 2)
	cfg := &config.Config{GowaWebhookSecret: "global-secret"}

	// Payload ala gowa: device_id = JID nomor WA, mis. 628977700129@s.whatsapp.net.
	body := webhookBody("628977700129@s.whatsapp.net")

	tid, err := resolveWebhookTenant(body, sign(secret2, body), st, cfg)
	if err != nil || tid != 2 {
		t.Fatalf("signature tenant2 (device JID): tid=%d err=%v", tid, err)
	}
	tid, err = resolveWebhookTenant(body, sign(secret1, body), st, cfg)
	if err != nil || tid != 1 {
		t.Fatalf("signature tenant1 (device JID): tid=%d err=%v", tid, err)
	}

	// Secret global + satu-tenant legacy: dua tenant aktif → device tak dikenal
	// harus ditolak (tidak jatuh ke tenant acak).
	_, err = resolveWebhookTenant(body, sign("global-secret", body), st, cfg)
	if err == nil {
		t.Fatal("global secret + multi-tenant + device tak dikenal harus ditolak")
	}
}
