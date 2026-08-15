package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func (r *Router) handleAdminCommand(ctx context.Context, admin *store.Customer, body string) {
	parts := strings.Fields(body)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/ringkasan", "/stats":
		r.adminSummary(ctx, admin)
	case "/menu":
		r.sendCatalog(ctx, admin)
	case "/blokir":
		r.adminBlock(ctx, admin, parts)
	case "/buka":
		r.adminUnblock(ctx, admin, parts)
	case "/produk":
		r.adminListProducts(ctx, admin)
	case "/bantuan", "/help":
		r.reply(ctx, admin, `📋 *Perintah Admin*
━━━━━━━━━━━━━━
/ringkasan — ringkasan order hari ini
/menu — kirim katalog ke nomor ini
/produk — daftar produk & stok
/blokir <nomor> — blokir customer
/buka <nomor> — buka blokir customer
/bantuan — tampilkan bantuan ini`)
	default:
		r.reply(ctx, admin, "Perintah tidak dikenal. Ketik */bantuan* untuk daftar perintah.")
	}
}

func (r *Router) adminSummary(ctx context.Context, admin *store.Customer) {
	stats, err := r.store.DashboardStats(ctx)
	if err != nil {
		r.reply(ctx, admin, "Gagal mengambil ringkasan.")
		return
	}
	r.reply(ctx, admin, fmt.Sprintf(`📊 *Ringkasan Hari Ini* (%s)
━━━━━━━━━━━━━━
🛒 Order baru: %d
💰 Pendapatan: %s
👥 Total customer: %d
⏳ Menunggu proses: %d
━━━━━━━━━━━━━━
Kelola lengkap di dashboard.`,
		time.Now().Format("02 Jan 2006"),
		stats.OrdersToday, FormatPrice(stats.RevenueToday), stats.TotalCustomers, stats.PendingOrders))
}

func (r *Router) adminBlock(ctx context.Context, admin *store.Customer, parts []string) {
	if len(parts) < 2 {
		r.reply(ctx, admin, "Format: */blokir <nomor>* (contoh: /blokir 628123456789)")
		return
	}
	phone := ExtractPhone(parts[1])
	c, err := r.store.GetCustomerByPhone(ctx, phone)
	if err != nil || c == nil {
		r.reply(ctx, admin, "Customer tidak ditemukan.")
		return
	}
	if err := r.store.SetCustomerStatus(ctx, c.ID, "blocked"); err != nil {
		r.reply(ctx, admin, "Gagal memblokir customer.")
		return
	}
	r.reply(ctx, admin, fmt.Sprintf("Customer %s (%s) diblokir.", c.Name, c.Phone))
}

func (r *Router) adminUnblock(ctx context.Context, admin *store.Customer, parts []string) {
	if len(parts) < 2 {
		r.reply(ctx, admin, "Format: */buka <nomor>* (contoh: /buka 628123456789)")
		return
	}
	phone := ExtractPhone(parts[1])
	c, err := r.store.GetCustomerByPhone(ctx, phone)
	if err != nil || c == nil {
		r.reply(ctx, admin, "Customer tidak ditemukan.")
		return
	}
	if err := r.store.SetCustomerStatus(ctx, c.ID, "active"); err != nil {
		r.reply(ctx, admin, "Gagal membuka blokir.")
		return
	}
	r.reply(ctx, admin, fmt.Sprintf("Customer %s (%s) dibuka kembali.", c.Name, c.Phone))
}

func (r *Router) adminListProducts(ctx context.Context, admin *store.Customer) {
	products, err := r.store.ListProducts(ctx, true)
	if err != nil {
		r.reply(ctx, admin, "Gagal mengambil daftar produk.")
		return
	}
	if len(products) == 0 {
		r.reply(ctx, admin, "Belum ada produk aktif.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📦 *Produk Aktif*\n━━━━━━━━━━━━━━\n")
	for _, p := range products {
		fmt.Fprintf(&b, "*%s* — %s\n   Stok: %s\n", p.Name, FormatPrice(p.Price), stockLabel(p.Stock))
	}
	r.reply(ctx, admin, b.String())
}

func stockLabel(stock int) string {
	if stock < 0 {
		return "∞"
	}
	return fmt.Sprintf("%d", stock)
}
