package dashboard

import (
	"context"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func (s *Server) actionLogin(c *fiber.Ctx) error {
	u := c.FormValue("username")
	p := c.FormValue("password")
	if u != s.cfg.DashboardUser || p != s.cfg.DashboardPassword {
		return redirect(c, "/admin/login", "Username atau password salah.", true)
	}
	s.setSession(c)
	return c.Redirect("/admin/overview")
}

func (s *Server) actionLogout(c *fiber.Ctx) error {
	s.clearSession(c)
	return c.Redirect("/admin/login")
}

func (s *Server) actionOrderStatus(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	status := c.FormValue("status")
	if !contains(orderStatuses, status) {
		return redirect(c, "/admin/orders", "Status tidak valid.", true)
	}
	if err := s.store.UpdateOrderStatus(ctx, id, status); err != nil {
		return redirect(c, "/admin/orders", "Gagal memperbarui status: "+err.Error(), true)
	}
	order, err := s.store.GetOrder(ctx, id)
	if err == nil && order != nil {
		go func() { _ = s.rtr.SendOrderStatusUpdate(context.Background(), order, status) }()
	}
	return redirect(c, "/admin/orders", "Status pesanan diperbarui.", false)
}

// ---------- products ----------

type productForm struct {
	Name        string
	Description string
	Price       int64
	Stock       int
	ImagePath   string
	IsActive    bool
}

func (s *Server) parseProduct(c *fiber.Ctx) (productForm, string) {
	f := productForm{
		Name:        strings.TrimSpace(c.FormValue("name")),
		Description: strings.TrimSpace(c.FormValue("description")),
		ImagePath:   strings.TrimSpace(c.FormValue("image_path")),
	}
	priceStr := strings.ReplaceAll(strings.TrimSpace(c.FormValue("price")), ".", "")
	p, err := strconv.ParseInt(priceStr, 10, 64)
	if err != nil || p < 0 {
		return f, "Harga tidak valid."
	}
	f.Price = p
	if st := strings.TrimSpace(c.FormValue("stock")); st != "" {
		iv, err := strconv.Atoi(st)
		if err != nil {
			return f, "Stok tidak valid."
		}
		f.Stock = iv
	} else {
		f.Stock = -1
	}
	f.IsActive = c.FormValue("is_active") != ""
	if f.Name == "" {
		return f, "Nama produk wajib diisi."
	}
	return f, ""
}

func (s *Server) actionProductCreate(c *fiber.Ctx) error {
	f, errMsg := s.parseProduct(c)
	if errMsg != "" {
		return redirect(c, "/admin/products", errMsg, true)
	}
	if _, err := s.store.CreateProduct(context.Background(), &store.Product{
		Name: f.Name, Description: f.Description, Price: f.Price,
		Stock: f.Stock, ImagePath: f.ImagePath, IsActive: true,
	}); err != nil {
		return redirect(c, "/admin/products", "Gagal menyimpan produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk ditambahkan.", false)
}

func (s *Server) actionProductUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	f, errMsg := s.parseProduct(c)
	if errMsg != "" {
		return redirect(c, "/admin/products", errMsg, true)
	}
	if err := s.store.UpdateProduct(context.Background(), &store.Product{
		ID: id, Name: f.Name, Description: f.Description, Price: f.Price,
		Stock: f.Stock, ImagePath: f.ImagePath, IsActive: f.IsActive,
	}); err != nil {
		return redirect(c, "/admin/products", "Gagal memperbarui produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk diperbarui.", false)
}

func (s *Server) actionProductDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	if err := s.store.DeleteProduct(context.Background(), id); err != nil {
		return redirect(c, "/admin/products", "Gagal menghapus produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk dihapus.", false)
}

// ---------- customers ----------

func (s *Server) actionCustomerStatus(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	status := c.FormValue("status")
	if status != "active" && status != "blocked" {
		return redirect(c, "/admin/customers", "Status tidak valid.", true)
	}
	if err := s.store.SetCustomerStatus(ctx, id, status); err != nil {
		return redirect(c, "/admin/customers", "Gagal memperbarui status: "+err.Error(), true)
	}
	return redirect(c, "/admin/customers", "Status pelanggan diperbarui.", false)
}

// ---------- broadcast ----------

func (s *Server) actionBroadcastCreate(c *fiber.Ctx) error {
	ctx := context.Background()
	msg := strings.TrimSpace(c.FormValue("message"))
	if msg == "" {
		return redirect(c, "/admin/broadcast", "Pesan broadcast wajib diisi.", true)
	}
	customers, err := s.store.ListCustomers(ctx, "")
	if err != nil {
		return redirect(c, "/admin/broadcast", "Gagal menghitung target: "+err.Error(), true)
	}
	targets := 0
	for _, cust := range customers {
		if cust.Status == "active" && cust.Phone != "" && cust.Phone != s.cfg.AdminPhone {
			targets++
		}
	}
	if _, err := s.store.CreateBroadcast(ctx, &store.Broadcast{
		Message: msg, Segment: "all", TotalTargets: targets,
	}); err != nil {
		return redirect(c, "/admin/broadcast", "Gagal membuat broadcast: "+err.Error(), true)
	}
	return redirect(c, "/admin/broadcast", "Broadcast antre. Dikirim otomatis oleh worker.", false)
}

// ---------- quick replies ----------

func (s *Server) actionReplyCreate(c *fiber.Ctx) error {
	kw := strings.TrimSpace(c.FormValue("keyword"))
	rp := strings.TrimSpace(c.FormValue("reply"))
	if kw == "" || rp == "" {
		return redirect(c, "/admin/replies", "Kata kunci dan balasan wajib diisi.", true)
	}
	if _, err := s.store.CreateQuickReply(context.Background(), &store.QuickReply{
		Keyword: kw, Reply: rp, IsActive: true,
	}); err != nil {
		return redirect(c, "/admin/replies", "Gagal menyimpan balasan: "+err.Error(), true)
	}
	return redirect(c, "/admin/replies", "Balasan cepat ditambahkan.", false)
}

func (s *Server) actionReplyToggle(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	replies, err := s.store.ListQuickReplies(ctx)
	if err != nil {
		return redirect(c, "/admin/replies", "Gagal memuat balasan: "+err.Error(), true)
	}
	for _, r := range replies {
		if r.ID == id {
			if err := s.store.UpdateQuickReply(ctx, &store.QuickReply{
				ID: r.ID, Keyword: r.Keyword, Reply: r.Reply, IsActive: !r.IsActive,
			}); err != nil {
				return redirect(c, "/admin/replies", "Gagal memperbarui balasan: "+err.Error(), true)
			}
			break
		}
	}
	return redirect(c, "/admin/replies", "Status balasan diperbarui.", false)
}

func (s *Server) actionReplyDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	if err := s.store.DeleteQuickReply(context.Background(), id); err != nil {
		return redirect(c, "/admin/replies", "Gagal menghapus balasan: "+err.Error(), true)
	}
	return redirect(c, "/admin/replies", "Balasan dihapus.", false)
}

// ---------- wa accounts ----------

func (s *Server) actionAccountCreate(c *fiber.Ctx) error {
	ctx := context.Background()
	username := strings.TrimSpace(c.FormValue("username"))
	password := c.FormValue("password")
	deviceID := strings.TrimSpace(c.FormValue("device_id"))
	if username == "" || password == "" {
		return redirect(c, "/admin/accounts", "Username dan password wajib diisi.", true)
	}

	// Verifikasi kredensial ke gowa; ambil device pertama jika ada.
	if id, err := s.gowa.Ping(ctx, username, password); err != nil {
		return redirect(c, "/admin/accounts", "Login gowa gagal: "+err.Error(), true)
	} else if deviceID == "" {
		deviceID = id
	}

	accounts, err := s.store.ListWAAccounts(ctx)
	if err != nil {
		return redirect(c, "/admin/accounts", "Gagal memuat akun: "+err.Error(), true)
	}
	isActive := c.FormValue("is_active") != "" || len(accounts) == 0

	id, err := s.store.UpsertWAAccount(ctx, username, password, deviceID, isActive)
	if err != nil {
		return redirect(c, "/admin/accounts", "Gagal menyimpan akun: "+err.Error(), true)
	}
	if isActive {
		if err := s.store.SetWAAccountActive(ctx, id); err != nil {
			return redirect(c, "/admin/accounts", "Akun tersimpan, tapi gagal diaktifkan: "+err.Error(), true)
		}
	}
	msg := "Akun disimpan."
	if deviceID == "" {
		msg += " Belum ada device — buat device di gowa atau isi Device ID."
	}
	return redirect(c, "/admin/accounts", msg, false)
}

func (s *Server) actionAccountActive(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	if err := s.store.SetWAAccountActive(context.Background(), id); err != nil {
		return redirect(c, "/admin/accounts", "Gagal mengaktifkan akun: "+err.Error(), true)
	}
	return redirect(c, "/admin/accounts", "Akun aktif.", false)
}

func (s *Server) actionAccountWebhook(c *fiber.Ctx) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	acc := s.findAccount(ctx, id)
	if acc == nil {
		return redirect(c, "/admin/accounts", "Akun tidak ditemukan.", true)
	}
	if acc.DeviceID == "" {
		return redirect(c, "/admin/accounts", "Device ID kosong — isi device ID dulu.", true)
	}
	if err := s.gowa.SetDeviceWebhook(ctx, acc.DeviceID, s.cfg.GowaWebhookURL, s.cfg.GowaWebhookSecret); err != nil {
		return redirect(c, "/admin/accounts", "Set webhook gagal: "+err.Error(), true)
	}
	return redirect(c, "/admin/accounts", "Webhook disetel.", false)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
