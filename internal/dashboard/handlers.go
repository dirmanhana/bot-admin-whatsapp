package dashboard

import (
	"context"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

var orderStatuses = []string{"baru", "diproses", "dikirim", "selesai", "batal"}

func (s *Server) pageLogin(c *fiber.Ctx) error {
	return s.render(c, "login", view{Title: "Masuk"})
}

func (s *Server) pageOverview(c *fiber.Ctx) error {
	ctx := context.Background()
	stats, err := s.store.DashboardStats(ctx)
	if err != nil {
		return redirect(c, "/admin/overview", "Gagal memuat statistik: "+err.Error(), true)
	}
	orders, err := s.store.ListOrders(ctx, "", 8)
	if err != nil {
		return redirect(c, "/admin/overview", "Gagal memuat pesanan: "+err.Error(), true)
	}
	return s.render(c, "overview", view{
		Title: "Ringkasan", Active: "overview",
		Data: map[string]any{"Stats": stats, "Orders": orders},
	})
}

func (s *Server) pageOrders(c *fiber.Ctx) error {
	ctx := context.Background()
	status := c.Query("status")
	orders, err := s.store.ListOrders(ctx, status, 200)
	if err != nil {
		return redirect(c, "/admin/orders", "Gagal memuat pesanan: "+err.Error(), true)
	}
	return s.render(c, "orders", view{
		Title: "Pesanan", Active: "orders",
		Data: map[string]any{"Orders": orders, "Statuses": orderStatuses, "Current": status},
	})
}

func (s *Server) pageProducts(c *fiber.Ctx) error {
	products, err := s.store.ListProducts(context.Background(), false)
	if err != nil {
		return redirect(c, "/admin/products", "Gagal memuat produk: "+err.Error(), true)
	}
	return s.render(c, "products", view{
		Title: "Produk", Active: "products",
		Data: map[string]any{"Products": products},
	})
}

func (s *Server) pageCustomers(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	customers, err := s.store.ListCustomers(context.Background(), q)
	if err != nil {
		return redirect(c, "/admin/customers", "Gagal memuat pelanggan: "+err.Error(), true)
	}
	return s.render(c, "customers", view{
		Title: "Pelanggan", Active: "customers",
		Data: map[string]any{"Customers": customers, "Q": q},
	})
}

func (s *Server) pageChat(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	cust, err := s.store.GetCustomer(ctx, id)
	if err != nil || cust == nil {
		return redirect(c, "/admin/customers", "Pelanggan tidak ditemukan.", true)
	}
	msgs, err := s.store.ListChatMessages(ctx, id, 100)
	if err != nil {
		return redirect(c, "/admin/customers", "Gagal memuat chat: "+err.Error(), true)
	}
	return s.render(c, "chat", view{
		Title: "Chat", Active: "customers",
		Data: map[string]any{"Customer": cust, "Messages": msgs},
	})
}

func (s *Server) pageBroadcast(c *fiber.Ctx) error {
	broadcasts, err := s.store.ListBroadcasts(context.Background())
	if err != nil {
		return redirect(c, "/admin/broadcast", "Gagal memuat broadcast: "+err.Error(), true)
	}
	return s.render(c, "broadcast", view{
		Title: "Broadcast", Active: "broadcast",
		Data: map[string]any{"Broadcasts": broadcasts},
	})
}

func (s *Server) pageReplies(c *fiber.Ctx) error {
	replies, err := s.store.ListQuickReplies(context.Background())
	if err != nil {
		return redirect(c, "/admin/replies", "Gagal memuat balasan: "+err.Error(), true)
	}
	return s.render(c, "replies", view{
		Title: "Balasan Cepat", Active: "replies",
		Data: map[string]any{"Replies": replies},
	})
}

type connInfo struct {
	DeviceID  string
	LoggedIn  bool
	Connected bool
}

func (s *Server) pageAccounts(c *fiber.Ctx) error {
	ctx := context.Background()
	accounts, err := s.store.ListWAAccounts(ctx)
	if err != nil {
		return redirect(c, "/admin/accounts", "Gagal memuat akun: "+err.Error(), true)
	}
	ci := connInfo{}
	if len(accounts) > 0 {
		// Status device dari akun aktif saja (EnsureToken memakai akun aktif).
		active := accounts[0]
		for _, a := range accounts {
			if a.IsActive {
				active = a
				break
			}
		}
		if active.DeviceID != "" {
			ci.DeviceID = active.DeviceID
			if logged, conn, err := s.gowa.DeviceStatus(ctx, active.DeviceID); err == nil {
				ci.LoggedIn = logged
				ci.Connected = conn
			}
		}
	}
	return s.render(c, "accounts", view{
		Title: "Akun WA", Active: "accounts",
		Data: map[string]any{"Accounts": accounts, "Connected": ci, "WebhookURL": s.cfg.GowaWebhookURL},
	})
}

func (s *Server) pageAccountQR(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	acc := s.findAccount(ctx, id)
	if acc == nil {
		return redirect(c, "/admin/accounts", "Akun tidak ditemukan.", true)
	}
	qr := struct{ URL string }{}
	if acc.DeviceID != "" {
		if link, _, err := s.gowa.LoginDeviceQR(ctx, acc.DeviceID); err == nil && link != "" {
			if strings.HasPrefix(link, "/") {
				link = s.cfg.GowaBaseURL + link
			}
			qr.URL = link
		}
	}
	return s.render(c, "qr", view{
		Title: "QR Login", Active: "accounts",
		Data: map[string]any{"Account": acc, "QR": qr},
	})
}

func (s *Server) findAccount(ctx context.Context, id int64) *store.WAAccount {
	accounts, err := s.store.ListWAAccounts(ctx)
	if err != nil {
		return nil
	}
	for i := range accounts {
		if accounts[i].ID == id {
			return &accounts[i]
		}
	}
	return nil
}
