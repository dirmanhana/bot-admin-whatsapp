package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/router"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

type Server struct {
	store *store.Store
	gowa  *gowaclient.Client
	cfg   *config.Config
	rtr   *router.Router
	tpl   *templates
}

func New(st *store.Store, g *gowaclient.Client, cfg *config.Config, rtr *router.Router) *Server {
	return &Server{store: st, gowa: g, cfg: cfg, rtr: rtr, tpl: loadTemplates()}
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
}

func (s *Server) render(c *fiber.Ctx, name string, v view) error {
	v.Store = s.cfg.StoreName
	v.User = s.cfg.DashboardUser
	v.Msg = c.Query("msg")
	v.Err = c.Query("err")
	v.Nav = navItems
	c.Set("Content-Type", "text/html; charset=utf-8")
	return s.tpl.ExecuteTemplate(c, name, v)
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
