package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/ai"
	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/settings"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

type Router struct {
	store    *store.Store
	gowa     *gowaclient.Client
	cfg      *config.Config
	ai       *ai.Service
	settings *settings.Service
	redact   bool
}

func New(st *store.Store, g *gowaclient.Client, cfg *config.Config, aiSvc *ai.Service, stg *settings.Service) *Router {
	return &Router{store: st, gowa: g, cfg: cfg, ai: aiSvc, settings: stg, redact: cfg.LogRedact}
}

// maskPhone menyamarkan nomor saat log redaksi aktif: 62812xxxx789.
func (r *Router) maskPhone(p string) string {
	if !r.redact || len(p) < 7 {
		return p
	}
	keep := 4
	return p[:keep] + strings.Repeat("x", len(p)-keep-3) + p[len(p)-3:]
}

// maskBody menyamarkan isi pesan saat log redaksi aktif.
func (r *Router) maskBody(s string) string {
	if !r.redact {
		return s
	}
	s = truncate(s, 60)
	return "[redaksi " + strconv.Itoa(len([]rune(s))) + " karakter]"
}

// storeSettings mengambil pengaturan toko; saat gagal, jatuh ke .env.
func (r *Router) storeSettings(ctx context.Context) *settings.Settings {
	st, err := r.settings.Get(ctx)
	if err != nil {
		return &settings.Settings{StoreName: r.cfg.StoreName, AdminPhone: r.cfg.AdminPhone}
	}
	return st
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
		log.Printf("webhook: unmarshal gagal: %v", err)
		return
	}
	log.Printf("webhook: event=%s device=%s session=%s", wp.Event, wp.DeviceID, wp.Session)
	if wp.Event != "message" || len(wp.Payload) == 0 {
		return
	}

	var m messagePayload
	if err := json.Unmarshal(wp.Payload, &m); err != nil {
		log.Printf("webhook: unmarshal payload gagal: %v", err)
		return
	}
	log.Printf("webhook: message from=%s chat=%s is_from_me=%v body=%q", r.maskPhone(m.From), r.maskPhone(m.ChatID), m.IsFromMe, r.maskBody(m.Body))
	if m.IsFromMe || m.From == "" {
		log.Printf("webhook: dilewati (is_from_me=%v from=%q)", m.IsFromMe, r.maskPhone(m.From))
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
	if phone == r.storeSettings(ctx).AdminPhone && strings.HasPrefix(text, "/") {
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

	// Jawaban AI berbasis knowledge base (katalog, produk, dll.) + memori percakapan
	if r.ai != nil {
// Kuota harian per pelanggan agar biaya AI terkendali.
	day := time.Now().Format("2006-01-02")
	quota := r.storeSettings(ctx).AIDailyQuota
	used, err := r.store.GetAIUsageCount(ctx, c.ID, day)
	if err == nil && quota > 0 && used >= quota {
		r.reply(ctx, c, "Maaf, pertanyaan gratis hari ini sudah habis. Hubungi admin untuk bantuan lebih lanjut.")
		return
	}
		historyLimit := 15
		if st := r.storeSettings(ctx); st.AIMaxHistory > 0 {
			historyLimit = st.AIMaxHistory
		}
		history, err := r.store.ListChatMessages(ctx, c.ID, historyLimit)
		if err != nil {
			history = nil
		}
		answer, err := r.ai.Answer(ctx, body, history)
		if err == nil && answer != "" {
			_ = r.store.IncrementAIUsage(ctx, c.ID, day)
			r.reply(ctx, c, answer)
			return
		}
	}

	r.reply(ctx, c, r.defaultReply(ctx))
}

func (r *Router) handleOrderSession(ctx context.Context, c *store.Customer, session *store.OrderSession, body, normalized string) {
	if normalized == "batal" || normalized == "cancel" {
		_ = r.store.DeleteOrderSession(ctx, c.ID)
		r.reply(ctx, c, "Pesanan dibatalkan. Ketik *menu* kapan saja untuk melihat katalog.")
		return
	}

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
			return
		}
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "entering_qty", ProductID: p.ID, Qty: 1, Items: session.Items,
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
		items := addToCart(session.Items, p.ID, p.Name, p.Price, qty)
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "more_or_checkout", Items: items,
		})
		r.reply(ctx, c, r.cartMessage(ctx, c, items)+"\n\n"+
			"Ketik nomor produk lain untuk menambah, *selesai* untuk lanjut ke pengiriman, atau *batal*.")

	case "more_or_checkout":
		products, err := r.store.ListProducts(ctx, true)
		idx := ParseNumber(normalized)
		if err == nil && idx > 0 && idx <= len(products) {
			p := products[idx-1]
			if p.Stock >= 0 && p.Stock <= 0 {
				r.reply(ctx, c, fmt.Sprintf("Maaf, %s sedang *habis*.", p.Name))
				return
			}
			_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
				CustomerID: c.ID, State: "entering_qty", ProductID: p.ID, Qty: 1, Items: session.Items,
			})
			r.reply(ctx, c, fmt.Sprintf("Anda memilih *%s* — %s.\n\nBerapa jumlah yang ingin dipesan? (mis. *2*)", p.Name, FormatPrice(p.Price)))
			return
		}
		if normalized == "selesai" || normalized == "checkout" || normalized == "jadi" || normalized == "ya" || normalized == "y" {
			if len(session.Items) == 0 {
				r.reply(ctx, c, "Keranjang masih kosong. Ketik *menu* untuk memilih produk.")
				return
			}
			_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
				CustomerID: c.ID, State: "choosing_delivery", Items: session.Items,
			})
			r.reply(ctx, c, "Pilih metode pengantaran:\n\n*1.* Kirim (antar) — ongkir dikenakan\n*2.* Ambil di toko (gratis)\n\nBalas *1* atau *2*, atau ketik *kirim*/*ambil*.")
			return
		}
		r.reply(ctx, c, "Ketik nomor produk untuk menambah, *selesai* untuk lanjut, atau *batal*.")

	case "choosing_delivery":
		delivery := ""
		switch {
		case normalized == "kirim" || normalized == "antar" || normalized == "1":
			delivery = "kirim"
		case normalized == "ambil" || normalized == "pickup" || normalized == "2":
			delivery = "ambil"
		}
		if delivery == "" {
			r.reply(ctx, c, "Balas *kirim* (antar) atau *ambil* (di toko).")
			return
		}
		if delivery == "ambil" {
			st := r.storeSettings(ctx)
			addr := "Ambil di toko"
			if st.StoreAddress != "" {
				addr += ": " + st.StoreAddress
			}
			_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
				CustomerID: c.ID, State: "confirming", Address: addr, DeliveryType: delivery, Items: session.Items,
			})
			r.sendOrderSummary(ctx, c, session.Items, addr, delivery)
			return
		}
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "entering_address", DeliveryType: delivery, Items: session.Items,
		})
		r.reply(ctx, c, "Boleh tahu *alamat pengiriman* Anda? (tulis alamat lengkap)")

	case "entering_address":
		if len(strings.TrimSpace(body)) < 5 {
			r.reply(ctx, c, "Alamat terlalu singkat. Mohon tulis alamat lengkap, atau *batal*.")
			return
		}
		_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
			CustomerID: c.ID, State: "confirming", Address: strings.TrimSpace(body), DeliveryType: session.DeliveryType, Items: session.Items,
		})
		r.sendOrderSummary(ctx, c, session.Items, strings.TrimSpace(body), session.DeliveryType)

	case "confirming":
		if normalized == "ya" || normalized == "y" || normalized == "yes" || normalized == "oke" || normalized == "ok" {
			r.createOrder(ctx, c, session)
			return
		}
		r.reply(ctx, c, "Ketik *ya* untuk konfirmasi pesanan, atau *batal* untuk membatalkan.")
	}
}

// addToCart menambahkan produk ke keranjang; bila produk sama sudah ada,
// jumlahnya ditambah.
func addToCart(items []store.OrderItem, productID int64, name string, price int64, qty int) []store.OrderItem {
	for i := range items {
		if items[i].ProductID == productID {
			items[i].Qty += qty
			return items
		}
	}
	return append(items, store.OrderItem{ProductID: productID, ProductName: name, Price: price, Qty: qty})
}

// cartMessage menyusun ringkasan keranjang (daftar item + subtotal).
func (r *Router) cartMessage(ctx context.Context, c *store.Customer, items []store.OrderItem) string {
	var b strings.Builder
	b.WriteString("🛒 *Keranjang Anda*\n━━━━━━━━━━━━━━\n")
	var total int64
	for i, it := range items {
		sub := it.Price * int64(it.Qty)
		total += sub
		fmt.Fprintf(&b, "%d. %s\n   %d x %s = %s\n", i+1, it.ProductName, it.Qty, FormatPrice(it.Price), FormatPrice(sub))
	}
	b.WriteString("━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "💰 *Subtotal: %s*", FormatPrice(total))
	return b.String()
}

func (r *Router) sendOrderSummary(ctx context.Context, c *store.Customer, items []store.OrderItem, address, deliveryType string) {
	st := r.storeSettings(ctx)
	var b strings.Builder
	b.WriteString("🛒 *Ringkasan Pesanan*\n━━━━━━━━━━━━━━\n")
	var total int64
	for _, it := range items {
		total += it.Price * int64(it.Qty)
		fmt.Fprintf(&b, "📦 %s\n🔢 %d x %s\n", it.ProductName, it.Qty, FormatPrice(it.Price))
	}
	fee := int64(0)
	if deliveryType != "ambil" {
		fee = st.DeliveryFee
	}
	if fee > 0 {
		fmt.Fprintf(&b, "🚚 Ongkir: %s\n", FormatPrice(fee))
	}
	b.WriteString("━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "💰 *Total: %s*\n\n", FormatPrice(total+fee))
	if deliveryType == "ambil" {
		b.WriteString("🏬 Pengambilan: *di toko*\n")
	}
	fmt.Fprintf(&b, "📍 Alamat: %s\n\nKetik *ya* untuk konfirmasi, atau *batal* untuk membatalkan.", address)
	r.reply(ctx, c, b.String())
}

func (r *Router) createOrder(ctx context.Context, c *store.Customer, session *store.OrderSession) {
	if len(session.Items) == 0 {
		_ = r.store.DeleteOrderSession(ctx, c.ID)
		r.reply(ctx, c, "Keranjang kosong. Ketik *menu* untuk memilih produk.")
		return
	}

	order, err := r.store.CreateOrder(ctx, c.ID, session.Address, session.DeliveryType, r.storeSettings(ctx).DeliveryFee, session.Items)
	if err != nil {
		r.reply(ctx, c, "Maaf, terjadi kendala saat memproses pesanan. Silakan coba lagi.")
		return
	}
	_ = r.store.DeleteOrderSession(ctx, c.ID)

	// Notify admin
	adminPhone := r.storeSettings(ctx).AdminPhone
	if adminPhone != "" {
		adminCust, err := r.store.GetCustomerByPhone(ctx, adminPhone)
		if err != nil || adminCust == nil {
			adminCust, _ = r.store.GetOrCreateCustomer(ctx, adminPhone, adminPhone+"@s.whatsapp.net", "Admin")
		}
		var itemsText strings.Builder
		for _, it := range order.Items {
			fmt.Fprintf(&itemsText, "🛒 %s x %d = %s\n", it.ProductName, it.Qty, FormatPrice(it.Price*int64(it.Qty)))
		}
		deliveryLine := "🏬 Pengambilan: di toko"
		if order.DeliveryType != "ambil" {
			deliveryLine = fmt.Sprintf("🚚 Kirim — ongkir: %s", FormatPrice(order.DeliveryFee))
		}
		msg := fmt.Sprintf(`📢 *ORDER BARU* %s
━━━━━━━━━━━━━━
👤 Nama: %s
📱 No: %s
%s💰 *Total: %s*
📍 Alamat: %s
%s
🕐 %s
━━━━━━━━━━━━━━
Kelola di dashboard atau balas /ringkasan.`,
			order.OrderNumber, c.Name, c.Phone, itemsText.String(),
			FormatPrice(order.Total), session.Address, deliveryLine, time.Now().Format("02 Jan 15:04"))
		if _, err := r.gowa.SendText(ctx, adminCust.Phone, msg); err != nil {
			// non-fatal
		}
	}

	st := r.storeSettings(ctx)
	var itemsText strings.Builder
	for _, it := range order.Items {
		fmt.Fprintf(&itemsText, "📦 %s x %d\n", it.ProductName, it.Qty)
	}
	r.reply(ctx, c, fmt.Sprintf(`✅ *Pesanan berhasil!*
━━━━━━━━━━━━━━
📋 No. Order: %s
%s💰 Total: %s
📍 Alamat: %s
━━━━━━━━━━━━━━
Kami akan segera memproses pesanan Anda. Terima kasih telah berbelanja di *%s*! 💖`,
		order.OrderNumber, itemsText.String(), FormatPrice(order.Total), session.Address, st.StoreName))
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
	st := r.storeSettings(ctx)
	msg := fmt.Sprintf("Halo %s! Pesanan *%s* Anda %s.\n\nTerima kasih telah berbelanja di *%s*! 💖",
		order.Customer.Name, order.OrderNumber, label, st.StoreName)
	_, err := r.gowa.SendText(ctx, WATarget(order.Customer), msg)
	return err
}

// ---------- helpers ----------

func (r *Router) reply(ctx context.Context, c *store.Customer, text string) {
	if _, err := r.gowa.SendText(ctx, WATarget(c), text); err != nil {
		log.Printf("reply ke %s gagal: %v", r.maskPhone(c.Phone), err)
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
	st := r.storeSettings(ctx)
	var b strings.Builder
	fmt.Fprintf(&b, "📦 *Katalog %s*\n━━━━━━━━━━━━━━\n", st.StoreName)
	for i, p := range products {
		fmt.Fprintf(&b, "%d. *%s*\n   %s", i+1, p.Name, FormatPrice(p.Price))
		if p.Description != "" {
			fmt.Fprintf(&b, "\n   %s", p.Description)
		}
		if p.Stock >= 0 {
			fmt.Fprintf(&b, "\n   Stok: %d", p.Stock)
		}
		if p.PurchaseLink != "" {
			fmt.Fprintf(&b, "\n   🔗 %s", p.PurchaseLink)
		}
		b.WriteString("\n")
	}
	b.WriteString("━━━━━━━━━━━━━━\nKetik nomor produk (mis. *1*) untuk memesan.")
	if st.StoreAddress != "" {
		fmt.Fprintf(&b, "\n📍 Alamat: %s", st.StoreAddress)
	}
	if st.AdminPhone != "" {
		fmt.Fprintf(&b, "\n📞 Kontak: %s", st.AdminPhone)
	}
	r.reply(ctx, c, b.String())

	// Pertahankan keranjang bila pelanggan sudah memulai pesanan.
	items := []store.OrderItem{}
	if existing, err := r.store.GetOrderSession(ctx, c.ID); err == nil && existing != nil && len(existing.Items) > 0 {
		items = existing.Items
	}
	_ = r.store.UpsertOrderSession(ctx, &store.OrderSession{
		CustomerID: c.ID, State: "selecting_product", Items: items,
	})
}

func (r *Router) defaultReply(ctx context.Context) string {
	st, err := r.settings.Get(ctx)
	storeName := r.cfg.StoreName
	if err == nil && st != nil && st.StoreName != "" {
		storeName = st.StoreName
	}
	return fmt.Sprintf("Halo! 👋 Untuk melihat katalog produk kami, ketik *menu*.\n\n"+
		"Kami di *%s* siap melayani Anda. Terima kasih! 💖", storeName)
}

// truncate limits a string for safe logging.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
