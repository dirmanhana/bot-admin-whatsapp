package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/dirman/bot-admin-whatsapp/internal/settings"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func (s *Server) actionLogin(c *fiber.Ctx) error {
	ip := c.IP()
	if s.isLoginLocked(ip) {
		return redirect(c, "/admin/login", "Terlalu banyak percobaan login. Coba lagi nanti.", true)
	}
	email := strings.TrimSpace(strings.ToLower(c.FormValue("username")))
	p := c.FormValue("password")
	t, err := s.store.GetTenantByEmail(c.Context(), email)
	if err != nil || t == nil || t.Status != "active" || !s.verifyTenantPassword(t, p) {
		s.recordLoginFail(ip)
		return redirect(c, "/admin/login", "Email atau password salah.", true)
	}
	s.clearLoginFails(ip)
	s.setSession(c, t.ID)
	return c.Redirect("/admin/overview")
}

func (s *Server) pageRegister(c *fiber.Ctx) error {
	if !s.cfg.AllowRegistration {
		return redirect(c, "/admin/login", "Pendaftaran toko baru ditutup.", true)
	}
	return s.render(c, "register", view{Title: "Daftar"})
}

// actionRegister membuat akun tenant baru (multi-user): email + password
// menjadi kredensial login dashboard toko tersebut.
func (s *Server) actionRegister(c *fiber.Ctx) error {
	if !s.cfg.AllowRegistration {
		return redirect(c, "/admin/login", "Pendaftaran toko baru ditutup.", true)
	}
	email := strings.TrimSpace(strings.ToLower(c.FormValue("email")))
	pw := c.FormValue("password")
	confirm := c.FormValue("password_confirm")
	storeName := strings.TrimSpace(c.FormValue("store_name"))
	if !strings.Contains(email, "@") || len(email) < 6 {
		return redirect(c, "/admin/register", "Email tidak valid.", true)
	}
	if len(pw) < 8 {
		return redirect(c, "/admin/register", "Password minimal 8 karakter.", true)
	}
	if pw != confirm {
		return redirect(c, "/admin/register", "Konfirmasi password tidak sama.", true)
	}
	existing, err := s.store.GetTenantByEmail(c.Context(), email)
	if err != nil {
		return redirect(c, "/admin/register", "Gagal memeriksa email: "+err.Error(), true)
	}
	if existing != nil {
		return redirect(c, "/admin/register", "Email sudah terdaftar. Silakan masuk.", true)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return redirect(c, "/admin/register", "Gagal membuat akun.", true)
	}
	tid, err := s.store.CreateTenant(c.Context(), email, string(hash))
	if err != nil {
		return redirect(c, "/admin/register", "Gagal membuat akun: "+err.Error(), true)
	}
	if storeName != "" {
		ctx := store.WithTenant(c.Context(), tid)
		_ = s.store.SetSetting(ctx, settings.KeyStoreName, storeName)
	}
	s.clearLoginFails(c.IP())
	s.setSession(c, tid)
	return redirect(c, "/admin/overview", "Akun berhasil dibuat. Selamat datang!", false)
}

func (s *Server) actionLogout(c *fiber.Ctx) error {
	s.clearSession(c)
	return c.Redirect("/admin/login")
}

func (s *Server) actionOrderStatus(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
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
		go func() { _ = s.rtr.SendOrderStatusUpdate(ctx, order, status) }()
	}
	return redirect(c, "/admin/orders", "Status pesanan diperbarui.", false)
}

// ---------- products ----------

type productForm struct {
	Name         string
	Description  string
	Price        int64
	Stock        int
	ImagePath    string
	IsActive     bool
	PurchaseLink string
}

func (s *Server) parseProduct(c *fiber.Ctx) (productForm, string) {
	f := productForm{
		Name:         strings.TrimSpace(c.FormValue("name")),
		Description:  strings.TrimSpace(c.FormValue("description")),
		ImagePath:    strings.TrimSpace(c.FormValue("image_path")),
		PurchaseLink: strings.TrimSpace(c.FormValue("purchase_link")),
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
	img, err := s.saveProductImage(c)
	if err != nil {
		return redirect(c, "/admin/products", err.Error(), true)
	}
	if img == "" {
		img = f.ImagePath
	}
	if _, err := s.store.CreateProduct(s.tenantCtx(c), &store.Product{
		Name: f.Name, Description: f.Description, Price: f.Price,
		Stock: f.Stock, ImagePath: img, IsActive: true,
		PurchaseLink: f.PurchaseLink,
	}); err != nil {
		return redirect(c, "/admin/products", "Gagal menyimpan produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk ditambahkan.", false)
}

func (s *Server) actionProductUpdate(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	f, errMsg := s.parseProduct(c)
	if errMsg != "" {
		return redirect(c, "/admin/products", errMsg, true)
	}
	existing, _ := s.store.GetProduct(ctx, id)
	img, err := s.saveProductImage(c)
	if err != nil {
		return redirect(c, "/admin/products", err.Error(), true)
	}
	if img != "" {
		if existing != nil {
			s.deleteProductImage(existing.ImagePath)
		}
	} else {
		img = f.ImagePath
	}
	if err := s.store.UpdateProduct(ctx, &store.Product{
		ID: id, Name: f.Name, Description: f.Description, Price: f.Price,
		Stock: f.Stock, ImagePath: img, IsActive: f.IsActive,
		PurchaseLink: f.PurchaseLink,
	}); err != nil {
		return redirect(c, "/admin/products", "Gagal memperbarui produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk diperbarui.", false)
}

func (s *Server) actionProductDelete(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	if existing, err := s.store.GetProduct(ctx, id); err == nil && existing != nil {
		s.deleteProductImage(existing.ImagePath)
	}
	if err := s.store.DeleteProduct(ctx, id); err != nil {
		return redirect(c, "/admin/products", "Gagal menghapus produk: "+err.Error(), true)
	}
	return redirect(c, "/admin/products", "Produk dihapus.", false)
}

// allowedImgExts adalah ekstensi gambar yang boleh diunggah.
var allowedImgExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true,
}

// saveProductImage menyimpan gambar produk ke direktori upload dan
// mengembalikan URL publik-nya. Bila tidak ada file, mengembalikan "".
func (s *Server) saveProductImage(c *fiber.Ctx) (string, error) {
	fh, err := c.FormFile("image")
	if err != nil {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if !allowedImgExts[ext] {
		return "", fmt.Errorf("format gambar tidak didukung (jpg, jpeg, png, gif, webp, avif)")
	}
	if fh.Size > 5*1024*1024 {
		return "", fmt.Errorf("gambar maksimal 5 MB")
	}
	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("p%d_%d%s", s.tenantID(c), time.Now().UnixNano(), ext)
	if err := c.SaveFile(fh, filepath.Join(s.cfg.UploadDir, name)); err != nil {
		return "", err
	}
	return "/admin/uploads/" + name, nil
}

// deleteProductImage menghapus file gambar produk lama bila merupakan
// hasil upload (bukan URL/path manual).
func (s *Server) deleteProductImage(path string) {
	if !strings.HasPrefix(path, "/admin/uploads/") {
		return
	}
	_ = os.Remove(filepath.Join(s.cfg.UploadDir, filepath.Base(path)))
}

// ---------- customers ----------

func (s *Server) actionCustomerStatus(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
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
	ctx := s.tenantCtx(c)
	msg := strings.TrimSpace(c.FormValue("message"))
	if msg == "" {
		return redirect(c, "/admin/broadcast", "Pesan broadcast wajib diisi.", true)
	}
	customers, err := s.store.ListCustomers(ctx, "")
	if err != nil {
		return redirect(c, "/admin/broadcast", "Gagal menghitung target: "+err.Error(), true)
	}
	targets := 0
	adminPhone := s.cfg.AdminPhone
	if st, err := s.settings.Get(ctx); err == nil && st.AdminPhone != "" {
		adminPhone = st.AdminPhone
	}
	for _, cust := range customers {
		if cust.Status == "active" && cust.Phone != "" && cust.Phone != adminPhone {
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
	if _, err := s.store.CreateQuickReply(s.tenantCtx(c), &store.QuickReply{
		Keyword: kw, Reply: rp, IsActive: true,
	}); err != nil {
		return redirect(c, "/admin/replies", "Gagal menyimpan balasan: "+err.Error(), true)
	}
	return redirect(c, "/admin/replies", "Balasan cepat ditambahkan.", false)
}

func (s *Server) actionReplyToggle(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
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
	if err := s.store.DeleteQuickReply(s.tenantCtx(c), id); err != nil {
		return redirect(c, "/admin/replies", "Gagal menghapus balasan: "+err.Error(), true)
	}
	return redirect(c, "/admin/replies", "Balasan dihapus.", false)
}

// ---------- wa accounts ----------

func (s *Server) actionAccountCreate(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
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
	if err := s.store.SetWAAccountActive(s.tenantCtx(c), id); err != nil {
		return redirect(c, "/admin/accounts", "Gagal mengaktifkan akun: "+err.Error(), true)
	}
	return redirect(c, "/admin/accounts", "Akun aktif.", false)
}

func (s *Server) actionAccountWebhook(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
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
	// Setiap device gowa memakai secret khusus tenant-nya; webhook masuk
	// diverifikasi terhadap secret ini (lihat resolveWebhookTenant).
	secret, err := s.store.TenantWebhookSecret(ctx, store.TenantID(ctx))
	if err != nil || secret == "" {
		return redirect(c, "/admin/accounts", "Webhook secret toko belum tersedia.", true)
	}
	if err := s.gowa.SetDeviceWebhook(ctx, acc.DeviceID, s.cfg.GowaWebhookURL, secret); err != nil {
		return redirect(c, "/admin/accounts", "Set webhook gagal: "+err.Error(), true)
	}
	return redirect(c, "/admin/accounts", "Webhook disetel dengan secret khusus toko ini.", false)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
