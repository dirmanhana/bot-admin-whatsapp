package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/ai"
	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

type Router struct {
	store *store.Store
	gowa  *gowaclient.Client
	cfg   *config.Config
	ai    *ai.Service
}

func New(st *store.Store, g *gowaclient.Client, cfg *config.Config, aiSvc *ai.Service) *Router {
	return &Router{store: st, gowa: g, cfg: cfg, ai: aiSvc}
}

// WebhookPayload is the envelope gowa POSTs to our webhook endpoint.
type WebhookPayload struct {
	Event    string          `json:"event"`
	DeviceID string          `json:"device_id"`
	Session  string          `json:"session_id"`
	Payload  json.RawMessage `json:"payload"`
}

type messagePayload struct {
	ID        string `json:"id"`
	IsFromMe  bool   `json:"is_from_me"`
	FromName  string `json:"from_name"`
	From      string `json:"from"`
	ChatID    string `json:"chat_id"`
	Body      string `json:"body"`
	Image     any    `json:"image"`
	Audio     any    `json:"audio"`
	Video     any    `json:"video"`
	Document  any    `json:"document"`
	Sticker   any    `json:"sticker"`
	Location  any    `json:"location"`
	Contact   any    `json:"contact"`
	Timestamp string `json:"timestamp"`
}

// HandleWebhook processes a single gowa webhook event. It runs synchronously;
// the HTTP handler should call it in a goroutine and return 200 immediately.
func (r *Router) HandleWebhook(ctx context.Context, body []byte) {
	var wp WebhookPayload
	if err := json.Unmarshal(body, &wp); err != nil {
		return
	}
	if wp.Event != "message" || len(wp.Payload) == 0 {
		return
	}

	var m messagePayload
	if err := json.Unmarshal(wp.Payload, &m); err != nil {
		return
	}
	if m.IsFromMe || m.From == "" {
		return
	}
	if strings.HasSuffix(m.ChatID, "@g.us") || strings.HasSuffix(m.ChatID, "@broadcast") {
		return
	}
	if m.From == "status@broadcast" || m.ChatID == "status@broadcast" {
		return
	}

	phone := ExtractPhone(m.From)
	if phone == "" {
		return
	}

	customer, err := r.store.GetOrCreateCustomer(ctx, phone, m.From, m.FromName)
	if err != nil {
		return
	}

	msgType := "text"
	if m.Body == "" {
		msgType = messageTypeOf(m)
	}
	if err := r.store.SaveChatMessage(ctx, &store.ChatMessage{
		CustomerID:  customer.ID,
		Direction:   "in",
		MessageType: msgType,
		Body:        m.Body,
		WAMessageID: m.ID,
	}); err != nil {
		return
	}

	if customer.Status == "blocked" {
		return
	}

	text := strings.TrimSpace(m.Body)
	if text == "" {
		return
	}

	// Admin commands
	if phone == r.cfg.AdminPhone && strings.HasPrefix(text, "/") {
		r.handleAdminCommand(ctx, customer, text)
		return
	}

	r.handleCustomerMessage(ctx, customer, text)
}

func (r *Router) handleCustomerMessage(ctx context.Context, c *store.Customer, body string) {
	normalized := Normalize(body)

	if normalized == "menu" || normalized == "start" || normalized == "katalog" || normalized == "mulai" {
		r.sendCatalog(ctx, c)
		return
	}
	if normalized == "batal" || normalized == "cancel" {
		_ = r.store.DeleteOrderSession(ctx, c.ID)
		r.reply(ctx, c, "Pesanan dibatalkan. Ketik *menu* kapan saja untuk melihat katalog.")
		return
	}

	session, err := r.store.GetOrderSession(ctx, c.ID)
	if err != nil {
		return
	}

	if session != nil {
		r.handleOrderSession(ctx, c, session, body, normalized)
		return
	}

	if qr, err := r.store.GetQuickReplyByKeyword(ctx, normalized); err == nil && qr != nil {
		r.reply(ctx, c, qr.Reply)
		return
	}

	// Jawaban AI berbasis knowledge base (katalog, produk, dll.)
	if r.ai != nil {
		answer, err := r.ai.Answer(ctx, body)
		if err == nil && answer != "" {
			r.reply(ctx, c, answer)
			return
		}
	}

	r.reply(ctx, c, r.defaultReply())
}

func (r *Router) handleOrderSession(ctx context.Context, c *store.Customer, session *store.OrderSession, body, normalized string) {
	switch session.State {
	case "selecting_product":
		idx := ParseNumber(normalized)
		if idx <= 0 {
			r.reply(ctx, c, "Ketik nomor produk yang ingin dipesan (mis. *1*), atau *batal*.")
			return
		}
		products, err := r.store.ListProducts(ctx, true)
		if err != nil || idx > len(products) {
			r.reply(ctx, c, "Nomor produk tidak valid. Ketik *menu* untuk melihat katalog.")
			return
		}
		p := products[idx-1]
		if p.Stock >= 0 && p.Stock <= 0 {
			r.reply(ctx, c, fmt.Sprintf("Maaf, %s sedang *habis*.", p.Name))
			_ = r.store.DeleteOrderSession(ctx, c.ID)
			return
		}
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "entering_qty", ProductID: p.ID, Qty: 1,
		})
		r.reply(ctx, c, fmt.Sprintf("Anda memilih *%s* — %s.\n\nBerapa jumlah yang ingin dipesan? (mis. *2*)", p.Name, FormatPrice(p.Price)))

	case "entering_qty":
		qty := ParseNumber(normalized)
		if qty <= 0 {
			r.reply(ctx, c, "Jumlah tidak valid. Masukkan angka (mis. *2*), atau *batal*.")
			return
		}
		p, err := r.store.GetProduct(ctx, session.ProductID)
		if err != nil || p == nil {
			_ = r.store.DeleteOrderSession(ctx, c.ID)
			r.reply(ctx, c, "Produk tidak ditemukan. Ketik *menu* untuk melihat katalog.")
			return
		}
		if p.Stock >= 0 && qty > p.Stock {
			r.reply(ctx, c, fmt.Sprintf("Maaf, stok %s hanya %d. Masukkan jumlah yang lebih kecil.", p.Name, p.Stock))
			return
		}
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "entering_address", ProductID: p.ID, Qty: qty,
		})
		r.reply(ctx, c, "Boleh tahu *alamat pengiriman* Anda? (tulis alamat lengkap)")

	case "entering_address":
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "confirming", ProductID: session.ProductID, Qty: session.Qty, Address: body,
		})
		p, err := r.store.GetProduct(ctx, session.ProductID)
		if err != nil || p == nil {
			_ = r.store.DeleteOrderSession(ctx, c.ID)
			r.reply(ctx, c, "Produk tidak ditemukan. Ketik *menu* untuk melihat katalog.")
			return
		}
		total := p.Price * int64(session.Qty)
		r.reply(ctx, c, fmt.Sprintf(`🛒 *Ringkasan Pesanan*
━━━━━━━━━━━━━━
📦 %s
🔢 %d x %s
━━━━━━━━━━━━━━
💰 *Total: %s*

📍 Alamat: %s

Ketik *ya* untuk konfirmasi, atau *batal* untuk membatalkan.`,
			p.Name, session.Qty, FormatPrice(p.Price), FormatPrice(total), body))

	case "confirming":
		if normalized == "ya" || normalized == "y" || normalized == "yes" || normalized == "oke" || normalized == "ok" {
			r.createOrder(ctx, c, session)
			return
		}
		r.reply(ctx, c, "Ketik *ya* untuk konfirmasi pesanan, atau *batal* untuk membatalkan.")
	}
}

func (r *Router) createOrder(ctx context.Context, c *store.Customer, session *store.OrderSession) {
	p, err := r.store.GetProduct(ctx, session.ProductID)
	if err != nil || p == nil {
		_ = r.store.DeleteOrderSession(ctx, c.ID)
		r.reply(ctx, c, "Produk tidak ditemukan. Ketik *menu* untuk melihat katalog.")
		return
	}

	order, err := r.store.CreateOrder(ctx, c.ID, session.Address, []store.OrderItem{{
		ProductID:   p.ID,
		ProductName: p.Name,
		Price:       p.Price,
		Qty:         session.Qty,
	}})
	if err != nil {
		r.reply(ctx, c, "Maaf, terjadi kendala saat memproses pesanan. Silakan coba lagi.")
		return
	}
	_ = r.store.DeleteOrderSession(ctx, c.ID)

	// Notify admin
	if r.cfg.AdminPhone != "" {
		adminCust, err := r.store.GetCustomerByPhone(ctx, r.cfg.AdminPhone)
		if err != nil || adminCust == nil {
			adminCust, _ = r.store.GetOrCreateCustomer(ctx, r.cfg.AdminPhone, r.cfg.AdminPhone+"@s.whatsapp.net", "Admin")
		}
		msg := fmt.Sprintf(`📢 *ORDER BARU* %s
━━━━━━━━━━━━━━
👤 Nama: %s
📱 No: %s
🛒 %s x %d = %s
💰 *Total: %s*
📍 Alamat: %s
🕐 %s
━━━━━━━━━━━━━━
Kelola di dashboard atau balas /ringkasan.`,
			order.OrderNumber, c.Name, c.Phone, p.Name, session.Qty, FormatPrice(p.Price),
			FormatPrice(order.Total), session.Address, time.Now().Format("02 Jan 15:04"))
		if _, err := r.gowa.SendText(ctx, adminCust.Phone, msg); err != nil {
			// non-fatal
		}
	}

	r.reply(ctx, c, fmt.Sprintf(`✅ *Pesanan berhasil!*
━━━━━━━━━━━━━━
📋 No. Order: %s
📦 %s x %d
💰 Total: %s
📍 Alamat: %s
━━━━━━━━━━━━━━
Kami akan segera memproses pesanan Anda. Terima kasih telah berbelanja di *%s*! 💖`,
		order.OrderNumber, p.Name, session.Qty, FormatPrice(order.Total), session.Address, r.cfg.StoreName))
}

// SendOrderStatusUpdate informs a customer that their order status changed.
func (r *Router) SendOrderStatusUpdate(ctx context.Context, order *store.Order, status string) error {
	if order.Customer == nil || order.Customer.Phone == "" {
		return fmt.Errorf("order tanpa customer")
	}
	if order.Customer.Status == "blocked" {
		return nil
	}
	label := map[string]string{
		"baru":     "telah kami terima ✅",
		"diproses": "sedang diproses 👨‍🍳",
		"dikirim":  "sedang dalam pengiriman 🚚",
		"selesai":  "telah selesai 🎉",
		"batal":    "dibatalkan ❌",
	}[status]
	if label == "" {
		label = status
	}
	msg := fmt.Sprintf("Halo %s! Pesanan *%s* Anda %s.\n\nTerima kasih telah berbelanja di *%s*! 💖",
		order.Customer.Name, order.OrderNumber, label, r.cfg.StoreName)
	_, err := r.gowa.SendText(ctx, order.Customer.Phone, msg)
	return err
}

// ---------- helpers ----------

func (r *Router) reply(ctx context.Context, c *store.Customer, text string) {
	if _, err := r.gowa.SendText(ctx, c.Phone, text); err != nil {
		return
	}
	_ = r.store.SaveChatMessage(ctx, &store.ChatMessage{
		CustomerID: c.ID, Direction: "out", MessageType: "text", Body: text,
	})
}

func (r *Router) sendCatalog(ctx context.Context, c *store.Customer) {
	products, err := r.store.ListProducts(ctx, true)
	if err != nil {
		return
	}
	if len(products) == 0 {
		r.reply(ctx, c, "Maaf, katalog sedang kosong. Silakan coba lagi nanti.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📦 *Katalog %s*\n━━━━━━━━━━━━━━\n", r.cfg.StoreName)
	for i, p := range products {
		fmt.Fprintf(&b, "%d. *%s*\n   %s", i+1, p.Name, FormatPrice(p.Price))
		if p.Description != "" {
			fmt.Fprintf(&b, "\n   %s", p.Description)
		}
		if p.Stock >= 0 {
			fmt.Fprintf(&b, "\n   Stok: %d", p.Stock)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "━━━━━━━━━━━━━━\nKetik nomor produk (mis. *1*) untuk memesan.")
	r.reply(ctx, c, b.String())

	_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
		CustomerID: c.ID, State: "selecting_product",
	})
}

func (r *Router) defaultReply() string {
	return fmt.Sprintf("Halo! 👋 Untuk melihat katalog produk kami, ketik *menu*.\n\n"+
		"Kami di *%s* siap melayani Anda. Terima kasih! 💖", r.cfg.StoreName)
}

func messageTypeOf(m messagePayload) string {
	switch {
	case m.Image != nil:
		return "image"
	case m.Audio != nil:
		return "audio"
	case m.Video != nil:
		return "video"
	case m.Document != nil:
		return "document"
	case m.Sticker != nil:
		return "sticker"
	case m.Location != nil:
		return "location"
	case m.Contact != nil:
		return "contact"
	default:
		return "unknown"
	}
}
