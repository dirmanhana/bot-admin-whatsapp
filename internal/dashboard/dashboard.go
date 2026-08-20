package dashboard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/dirman/bot-admin-whatsapp/internal/ai"
	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/router"
	"github.com/dirman/bot-admin-whatsapp/internal/settings"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

type Server struct {
	store    *store.Store
	gowa     *gowaclient.Client
	cfg      *config.Config
	rtr      *router.Router
	ai       *ai.Service
	settings *settings.Service
	tpl      *templates

	loginMu      sync.Mutex
	loginAttempts map[string]*loginAttempt
}

type loginAttempt struct {
	Fails    int
	LockedAt time.Time
}

func New(st *store.Store, g *gowaclient.Client, cfg *config.Config, rtr *router.Router, aiSvc *ai.Service, stg *settings.Service) *Server {
	return &Server{
		store: st, gowa: g, cfg: cfg, rtr: rtr, ai: aiSvc, settings: stg, tpl: loadTemplates(),
		loginAttempts: map[string]*loginAttempt{},
	}
}

// isLoginLocked menolak login dari IP yang terlalu sering gagal.
func (s *Server) isLoginLocked(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	a := s.loginAttempts[ip]
	if a == nil {
		return false
	}
	// Belum terkunci sampai jumlah gagal mencapai batas.
	if a.Fails < s.cfg.LoginMaxAttempts {
		return false
	}
	// Kunci kedaluwarsa setelah beberapa menit.
	if time.Since(a.LockedAt) > time.Duration(s.cfg.LoginLockoutMinutes)*time.Minute {
		delete(s.loginAttempts, ip)
		return false
	}
	return true
}

func (s *Server) recordLoginFail(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	a := s.loginAttempts[ip]
	if a == nil {
		a = &loginAttempt{}
		s.loginAttempts[ip] = a
	}
	a.Fails++
	if a.Fails >= s.cfg.LoginMaxAttempts {
		a.LockedAt = time.Now()
	}
}

func (s *Server) clearLoginFails(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginAttempts, ip)
}

// passwordHash mengembalikan hash bcrypt password dashboard bila sudah diubah
// lewat Pengaturan; kosong bila masih memakai DASHBOARD_PASSWORD dari .env.
func (s *Server) passwordHash(ctx context.Context) string {
	kv, err := s.store.GetSettings(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(kv["dashboard_password_hash"])
}

// verifyPassword memeriksa kata sandi dashboard terhadap hash DB (bila ada)
// atau password dari .env.
func (s *Server) verifyPassword(ctx context.Context, password string) bool {
	if hash := s.passwordHash(ctx); hash != "" {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return password == s.cfg.DashboardPassword
}

func (s *Server) Register(app *fiber.App) {
	app.Get("/admin/static/*", func(c *fiber.Ctx) error {
		file := strings.TrimPrefix(c.Params("*"), "/")
		if file == "" {
			return c.Status(fiber.StatusNotFound).SendString("not found")
		}
		b, err := webFS.ReadFile("web/" + file)
		if err != nil {
			return c.Status(fiber.StatusNotFound).SendString("not found")
		}
		ct := mime.TypeByExtension(path.Ext(file))
		if ct == "" {
			ct = "application/octet-stream"
		}
		c.Set("Content-Type", ct)
		return c.Send(b)
	})

	admin := app.Group("/admin")

	admin.Get("/login", s.pageLogin)
	admin.Post("/login", s.actionLogin)
	admin.Post("/logout", s.actionLogout)

	admin.Use(s.requireAuth)

	admin.Get("/", func(c *fiber.Ctx) error { return c.Redirect("/admin/overview") })
	admin.Get("/overview", s.pageOverview)

	admin.Get("/orders", s.pageOrders)
	admin.Post("/orders/:id/status", s.actionOrderStatus)

	admin.Get("/products", s.pageProducts)
	admin.Post("/products", s.actionProductCreate)
	admin.Post("/products/:id", s.actionProductUpdate)
	admin.Post("/products/:id/delete", s.actionProductDelete)

	admin.Get("/customers", s.pageCustomers)
	admin.Post("/customers/:id/status", s.actionCustomerStatus)
	admin.Get("/customers/:id/chat", s.pageChat)

	admin.Get("/broadcast", s.pageBroadcast)
	admin.Post("/broadcast", s.actionBroadcastCreate)

	admin.Get("/replies", s.pageReplies)
	admin.Post("/replies", s.actionReplyCreate)
	admin.Post("/replies/:id/toggle", s.actionReplyToggle)
	admin.Post("/replies/:id/delete", s.actionReplyDelete)

	admin.Get("/accounts", s.pageAccounts)
	admin.Post("/accounts", s.actionAccountCreate)
	admin.Post("/accounts/:id/active", s.actionAccountActive)
	admin.Post("/accounts/:id/webhook", s.actionAccountWebhook)
	admin.Get("/accounts/:id/qr", s.pageAccountQR)

	admin.Get("/ai", s.pageAI)
	admin.Post("/ai", s.actionAISave)
	admin.Post("/ai/test", s.actionAITest)
	admin.Post("/ai/knowledge", s.actionKnowledgeUpload)
	admin.Post("/ai/knowledge/:id/delete", s.actionKnowledgeDelete)

	admin.Get("/settings", s.pageSettings)
	admin.Post("/settings", s.actionSettingsSave)

	// Gambar produk hasil upload (disimpan di cfg.UploadDir).
	app.Get("/admin/uploads/*", s.serveUpload)
}

// serveUpload mengirim file gambar produk dari direktori upload.
// Nama file diambil dengan filepath.Base agar path traversal tidak mungkin.
func (s *Server) serveUpload(c *fiber.Ctx) error {
	name := filepath.Base(c.Params("*"))
	if name == "" || name == "." {
		return c.Status(fiber.StatusNotFound).SendString("file tidak ditemukan")
	}
	if !allowedImgExts[strings.ToLower(filepath.Ext(name))] {
		return c.Status(fiber.StatusNotFound).SendString("file tidak ditemukan")
	}
	p := filepath.Join(s.cfg.UploadDir, name)
	if _, err := os.Stat(p); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("file tidak ditemukan")
	}
	return c.SendFile(p)
}

// ---------- auth ----------

const sessionCookie = "admin_session"

func (s *Server) sign(v string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) sessionValue() string {
	return s.cfg.DashboardUser + "." + s.sign(s.cfg.DashboardUser)
}

func (s *Server) requireAuth(c *fiber.Ctx) error {
	if c.Cookies(sessionCookie) == s.sessionValue() {
		return c.Next()
	}
	return c.Redirect("/admin/login")
}

func (s *Server) setSession(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    s.sessionValue(),
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

func (s *Server) clearSession(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Lax",
		Expires:  time.Unix(0, 0),
	})
}

// ---------- view helpers ----------

type view struct {
	Title  string
	Active string
	Store  string
	User   string
	Msg    string
	Err    string
	Banner string
	Nav    []navItem
	Data   any
}

type navItem struct {
	Key   string
	Href  string
	Label string
	Ico   string
}

var navItems = []navItem{
	{"overview", "/admin/overview", "Ringkasan", "dashboard"},
	{"orders", "/admin/orders", "Pesanan", "cart"},
	{"products", "/admin/products", "Produk", "package"},
	{"customers", "/admin/customers", "Pelanggan", "users"},
	{"broadcast", "/admin/broadcast", "Broadcast", "megaphone"},
	{"replies", "/admin/replies", "Balasan Cepat", "zap"},
	{"accounts", "/admin/accounts", "Akun WA", "phone"},
	{"ai", "/admin/ai", "AI & Data", "sparkles"},
	{"settings", "/admin/settings", "Pengaturan", "settings"},
}

func (s *Server) storeName() string {
	if st, err := s.settings.Get(context.Background()); err == nil && st.StoreName != "" {
		return st.StoreName
	}
	return s.cfg.StoreName
}

func (s *Server) render(c *fiber.Ctx, name string, v view) error {
	v.Store = s.storeName()
	v.User = s.cfg.DashboardUser
	v.Msg = c.Query("msg")
	v.Err = c.Query("err")
	v.Banner = s.securityBanner()
	v.Nav = navItems
	c.Set("Content-Type", "text/html; charset=utf-8")
	return s.tpl.ExecuteTemplate(c, name, v)
}

// securityBanner mengingatkan admin bila memakai kredensial/secret default.
func (s *Server) securityBanner() string {
	ctx := context.Background()
	if s.passwordHash(ctx) == "" && s.cfg.DashboardPassword == "admin123" {
		return "Password dashboard masih default (admin123). Ubah lewat menu Pengaturan &gt; Keamanan."
	}
	if s.cfg.SessionSecret == "insecure-session-secret" {
		return "SESSION_SECRET masih default. Set nilai acak di .env untuk produksi."
	}
	return ""
}

func redirect(c *fiber.Ctx, path, msg string, isErr bool) error {
	if msg != "" {
		key := "msg"
		if isErr {
			key = "err"
		}
		path += "?" + key + "=" + url.QueryEscape(msg)
	}
	return c.Redirect(path)
}
