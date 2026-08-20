package store

import (
	"context"
	"strings"
	"testing"
)

// testDSNWithDB mengembalikan DSN test untuk database tertentu.
func testDSNWithDB(dbName string) string {
	base := testDSN()
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		rest := base[idx+1:]
		if q := strings.Index(rest, "?"); q >= 0 {
			return base[:idx+1] + dbName + rest[q:]
		}
		return base[:idx+1] + dbName
	}
	return base
}

func TestMigrateLegacyToMultiTenant(t *testing.T) {
	// Simulasikan DB lama (sebelum 0007): database sementara diisi migrasi
	// 0001–0006 + data, lalu Migrate() menerapkan 0007 dan data lama otomatis
	// milik tenant 1. Database khusus agar skemanya benar-benar fresh.
	admin, err := Open(context.Background(), testDSN())
	if err != nil {
		t.Skipf("postgres tidak tersedia, skip: %v", err)
	}
	defer admin.Close()
	resetSchema(t, admin)

	dbName := "bot_admin_whatsapp_legacy_test"
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + dbName); err != nil {
		t.Fatalf("drop legacy db: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + dbName); err != nil {
		t.Fatalf("create legacy db: %v", err)
	}
	defer func() { _, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + dbName) }()

	db, err := Open(context.Background(), testDSNWithDB(dbName))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()

	legacy := []string{
		"0001_init.sql", "0002_ai.sql", "0003_product_ai.sql",
		"0004_drop_product_ai_prompt.sql", "0005_cart_delivery_usage.sql", "0006_order_session_delivery.sql",
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	for _, name := range legacy {
		b, err := migrationsFS.ReadFile("migrations/postgres/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			t.Fatalf("mark %s: %v", name, err)
		}
	}

	// Data lama.
	if _, err := db.Exec(`INSERT INTO customers (phone, jid, name) VALUES ('628111', '628111@s.whatsapp.net', 'Budi')`); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO products (name, price, stock) VALUES ('Kopi', 15000, 10)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('store_address', 'Jl. Lama 1')`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	// Terapkan migrasi selanjutnya (0007).
	st := New(db)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate 0007: %v", err)
	}

	// Data lama harus milik tenant 1.
	ctx := WithTenant(context.Background(), 1)
	if _, err := st.GetCustomerByPhone(ctx, "628111"); err != nil {
		t.Fatalf("customer tenant1: %v", err)
	}
	prods, err := st.ListProducts(ctx, false)
	if err != nil || len(prods) != 1 || prods[0].Name != "Kopi" {
		t.Fatalf("products tenant1 = %v (%v)", prods, err)
	}
	kv, err := st.GetSettings(ctx)
	if err != nil || kv["store_address"] != "Jl. Lama 1" {
		t.Fatalf("settings tenant1 = %v (%v)", kv, err)
	}
	// Tenant 2 tidak boleh melihat data lama.
	ctx2 := WithTenant(context.Background(), 2)
	if c, _ := st.GetCustomerByPhone(ctx2, "628111"); c != nil {
		t.Fatalf("tenant2 melihat customer tenant1: %+v", c)
	}
	if p, _ := st.ListProducts(ctx2, false); len(p) != 0 {
		t.Fatalf("tenant2 melihat produk tenant1: %+v", p)
	}
}

func TestTenantIsolation(t *testing.T) {
	_, st := openTestDB(t)
	ctx1 := WithTenant(context.Background(), 1)
	ctx2 := WithTenant(context.Background(), 2)

	// Tenant 2 harus dibuat dulu (mis. lewat registrasi).
	if _, err := st.CreateTenant(context.Background(), "toko2@example.com", "hash2"); err != nil {
		t.Fatalf("create tenant2: %v", err)
	}

	// Produk per tenant.
	if _, err := st.CreateProduct(ctx1, &Product{Name: "Produk T1", Price: 1000, Stock: 5, IsActive: true}); err != nil {
		t.Fatalf("create product t1: %v", err)
	}
	if _, err := st.CreateProduct(ctx2, &Product{Name: "Produk T2", Price: 2000, Stock: 5, IsActive: true}); err != nil {
		t.Fatalf("create product t2: %v", err)
	}
	p1, _ := st.ListProducts(ctx1, false)
	p2, _ := st.ListProducts(ctx2, false)
	if len(p1) != 1 || p1[0].Name != "Produk T1" {
		t.Fatalf("produk t1 = %+v", p1)
	}
	if len(p2) != 1 || p2[0].Name != "Produk T2" {
		t.Fatalf("produk t2 = %+v", p2)
	}

	// Customer dengan nomor sama boleh ada di dua tenant berbeda.
	c1, err := st.GetOrCreateCustomer(ctx1, "628123456789", "628123456789@s.whatsapp.net", "Satu")
	if err != nil {
		t.Fatalf("gocc t1: %v", err)
	}
	c2, err := st.GetOrCreateCustomer(ctx2, "628123456789", "628123456789@s.whatsapp.net", "Dua")
	if err != nil {
		t.Fatalf("gocc t2: %v", err)
	}
	if c1.ID == c2.ID {
		t.Fatalf("customer harus terpisah per tenant: t1=%d t2=%d", c1.ID, c2.ID)
	}
	if c1.Name != "Satu" || c2.Name != "Dua" {
		t.Fatalf("nama customer tertukar: %q vs %q", c1.Name, c2.Name)
	}

	// Order + nomor order unik per tenant (masing-masing mulai 0001).
	o1, err := st.CreateOrder(ctx1, c1.ID, "Jl. A", "kirim", 5000, []OrderItem{{ProductID: p1[0].ID, ProductName: "Produk T1", Price: 1000, Qty: 2}})
	if err != nil {
		t.Fatalf("create order t1: %v", err)
	}
	o2, err := st.CreateOrder(ctx2, c2.ID, "Jl. B", "ambil", 0, []OrderItem{{ProductID: p2[0].ID, ProductName: "Produk T2", Price: 2000, Qty: 1}})
	if err != nil {
		t.Fatalf("create order t2: %v", err)
	}
	if !strings.HasSuffix(o1.OrderNumber, "-0001") || !strings.HasSuffix(o2.OrderNumber, "-0001") {
		t.Fatalf("nomor order per tenant salah: %s vs %s", o1.OrderNumber, o2.OrderNumber)
	}
	// Isolasi: order tenant 2 tidak boleh terlihat dari tenant 1.
	orders1, _ := st.ListOrders(ctx1, "", 10)
	if len(orders1) != 1 || orders1[0].ID != o1.ID {
		t.Fatalf("orders t1 = %+v", orders1)
	}
	if ord, _ := st.GetOrder(ctx1, o2.ID); ord != nil {
		t.Fatalf("tenant1 bisa membaca order tenant2: %+v", ord)
	}

	// Settings per tenant.
	if err := st.SetSetting(ctx1, "store_name", "Toko Satu"); err != nil {
		t.Fatalf("set settings t1: %v", err)
	}
	if err := st.SetSetting(ctx2, "store_name", "Toko Dua"); err != nil {
		t.Fatalf("set settings t2: %v", err)
	}
	kv1, _ := st.GetSettings(ctx1)
	kv2, _ := st.GetSettings(ctx2)
	if kv1["store_name"] != "Toko Satu" || kv2["store_name"] != "Toko Dua" {
		t.Fatalf("settings bocor: %v vs %v", kv1, kv2)
	}

	// Quick replies unik per tenant.
	if _, err := st.CreateQuickReply(ctx1, &QuickReply{Keyword: "info", Reply: "info toko satu", IsActive: true}); err != nil {
		t.Fatalf("qr t1: %v", err)
	}
	if _, err := st.CreateQuickReply(ctx2, &QuickReply{Keyword: "info", Reply: "info toko dua", IsActive: true}); err != nil {
		t.Fatalf("qr t2: %v", err)
	}
	q1, _ := st.GetQuickReplyByKeyword(ctx1, "info")
	q2, _ := st.GetQuickReplyByKeyword(ctx2, "info")
	if q1 == nil || q2 == nil || q1.Reply == q2.Reply {
		t.Fatalf("quick reply bocor: %+v vs %+v", q1, q2)
	}

	// WA account unik per tenant + resolusi device → tenant.
	if _, err := st.UpsertWAAccount(ctx1, "user1", "pass1", "dev-1", true); err != nil {
		t.Fatalf("wa t1: %v", err)
	}
	if _, err := st.UpsertWAAccount(ctx2, "user1", "pass2", "dev-2", true); err != nil {
		t.Fatalf("wa t2: %v", err)
	}
	tid1, _ := st.TenantIDByDeviceID(context.Background(), "dev-1")
	tid2, _ := st.TenantIDByDeviceID(context.Background(), "dev-2")
	if tid1 != 1 || tid2 != 2 {
		t.Fatalf("resolve device salah: dev-1→%d, dev-2→%d", tid1, tid2)
	}

	// Device yang sama tidak boleh dipakai tenant lain (sumber kebocoran).
	if _, err := st.UpsertWAAccount(ctx2, "user2", "pass2", "dev-1", true); err == nil {
		t.Fatal("device dev-1 milik tenant1 harus ditolak untuk tenant2")
	}

	// Order session per tenant (pelanggan sama, toko beda).
	if err := st.UpsertOrderSession(ctx1, &OrderSession{CustomerID: c1.ID, State: "selecting_product"}); err != nil {
		t.Fatalf("session t1: %v", err)
	}
	s2, err := st.GetOrderSession(ctx2, c2.ID)
	if err != nil || s2 != nil {
		t.Fatalf("session t2 harus kosong, dapat %+v (%v)", s2, err)
	}

	// Daftar tenant aktif untuk broadcast worker.
	ids, err := st.ListTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("list tenant: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("tenant ids = %v", ids)
	}
}

func TestCreateOrderSequencePerTenant(t *testing.T) {
	_, st := openTestDB(t)
	ctx1 := WithTenant(context.Background(), 1)
	ctx2 := WithTenant(context.Background(), 2)
	if _, err := st.CreateTenant(context.Background(), "t2@example.com", "h2"); err != nil {
		t.Fatalf("create tenant2: %v", err)
	}
	c1, _ := st.GetOrCreateCustomer(ctx1, "62811", "62811@s.whatsapp.net", "A")
	c2, _ := st.GetOrCreateCustomer(ctx2, "62822", "62822@s.whatsapp.net", "B")
	p1, err := st.CreateProduct(ctx1, &Product{Name: "a", Price: 1, Stock: -1, IsActive: true})
	if err != nil {
		t.Fatalf("product t1: %v", err)
	}
	p2, err := st.CreateProduct(ctx2, &Product{Name: "b", Price: 1, Stock: -1, IsActive: true})
	if err != nil {
		t.Fatalf("product t2: %v", err)
	}

	o1a, err := st.CreateOrder(ctx1, c1.ID, "x", "kirim", 0, []OrderItem{{ProductID: p1, ProductName: "a", Price: 1, Qty: 1}})
	if err != nil {
		t.Fatalf("order t1 #1: %v", err)
	}
	o1b, err := st.CreateOrder(ctx1, c1.ID, "x", "kirim", 0, []OrderItem{{ProductID: p1, ProductName: "a", Price: 1, Qty: 1}})
	if err != nil {
		t.Fatalf("order t1 #2: %v", err)
	}
	o2a, err := st.CreateOrder(ctx2, c2.ID, "y", "kirim", 0, []OrderItem{{ProductID: p2, ProductName: "b", Price: 1, Qty: 1}})
	if err != nil {
		t.Fatalf("order t2 #1: %v", err)
	}
	// Tiap tenant punya urutan sendiri mulai dari 1.
	if !strings.HasSuffix(o1a.OrderNumber, "-0001") || !strings.HasSuffix(o1b.OrderNumber, "-0002") || !strings.HasSuffix(o2a.OrderNumber, "-0001") {
		t.Fatalf("nomor order salah: %s, %s, %s", o1a.OrderNumber, o1b.OrderNumber, o2a.OrderNumber)
	}
}

func TestCreateOrderDecrementsStock(t *testing.T) {
	_, st := openTestDB(t)
	ctx := WithTenant(context.Background(), 1)
	c, _ := st.GetOrCreateCustomer(ctx, "62811", "62811@s.whatsapp.net", "A")

	// Produk stok terbatas.
	pid, err := st.CreateProduct(ctx, &Product{Name: "Terbatas", Price: 100, Stock: 5, IsActive: true})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	// Produk stok tak terbatas (-1).
	unlim, err := st.CreateProduct(ctx, &Product{Name: "Tanpa Batas", Price: 200, Stock: -1, IsActive: true})
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	// Pesan 3 dari stok 5.
	if _, err := st.CreateOrder(ctx, c.ID, "addr", "kirim", 0, []OrderItem{{ProductID: pid, ProductName: "Terbatas", Price: 100, Qty: 3}}); err != nil {
		t.Fatalf("order: %v", err)
	}
	p, _ := st.GetProduct(ctx, pid)
	if p.Stock != 2 {
		t.Fatalf("stok setelah order harus 2, dapat %d", p.Stock)
	}
	// Produk tak terbatas tidak berubah.
	p2, _ := st.GetProduct(ctx, unlim)
	if p2.Stock != -1 {
		t.Fatalf("stok tak terbatas harus tetap -1, dapat %d", p2.Stock)
	}

	// Pesan melebihi stok harus gagal (rollback total).
	_, err = st.CreateOrder(ctx, c.ID, "addr", "kirim", 0, []OrderItem{{ProductID: pid, ProductName: "Terbatas", Price: 100, Qty: 99}})
	if err == nil {
		t.Fatal("order dengan stok kurang harus gagal")
	}
	// Stok & jumlah order tidak berubah setelah kegagalan.
	p, _ = st.GetProduct(ctx, pid)
	if p.Stock != 2 {
		t.Fatalf("stok berubah setelah order gagal: %d", p.Stock)
	}
	orders, _ := st.ListOrders(ctx, "", 10)
	if len(orders) != 1 {
		t.Fatalf("jumlah order harus tetap 1, dapat %d", len(orders))
	}
}
