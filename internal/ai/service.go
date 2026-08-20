package ai

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/cryptx"
	"github.com/dirman/bot-admin-whatsapp/internal/settings"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

// Settings adalah konfigurasi AI yang disimpan di tabel settings.
type Settings struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
}

const (
	keyProvider = "ai_provider"
	keyBaseURL  = "ai_base_url"
	keyAPIKey   = "ai_api_key"
	keyModel    = "ai_model"
	keyEnabled  = "ai_enabled"
)

// Service menyediakan jawaban AI berbasis knowledge base (RAG sederhana).
type Service struct {
	store  *store.Store
	cfg    *config.Config
	stg    *settings.Service
	cipher *cryptx.Cipher

	mu       sync.Mutex
	cached   *Settings
	cachedAt time.Time

	cacheMu sync.Mutex
	cache   map[string]cacheEntry
}

// cacheEntry menyimpan jawaban untuk pertanyaan yang sama agar tidak
// memanggil API berulang (hemat biaya). TTL 2 jam.
type cacheEntry struct {
	answer    string
	cachedAt  time.Time
}

const cacheTTL = 2 * time.Hour

func New(st *store.Store, cfg *config.Config, stg *settings.Service, cipher *cryptx.Cipher) *Service {
	return &Service{store: st, cfg: cfg, stg: stg, cipher: cipher, cache: map[string]cacheEntry{}}
}

// Settings memuat konfigurasi AI; nilai dari database menang atas .env.
// Di-cache 60 detik agar tidak membebani DB per pesan.
func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil && time.Since(s.cachedAt) < 60*time.Second {
		return s.cached, nil
	}
	kv, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	prov := firstNonEmpty(kv[keyProvider], s.cfg.AIProvider)
	baseURL := firstNonEmpty(kv[keyBaseURL], s.cfg.AIBaseURL)
	model := firstNonEmpty(kv[keyModel], s.cfg.AIModel)
	if p := ProviderByKey(prov); p != nil {
		if baseURL == "" {
			baseURL = p.BaseURL
		}
		if model == "" {
			model = p.Model
		}
	}
	apiKey := kv[keyAPIKey]
	if s.cipher != nil && apiKey != "" {
		if k, err := s.cipher.Decrypt(apiKey); err == nil {
			apiKey = k
		}
	}
	st := &Settings{
		Provider: prov,
		BaseURL:  baseURL,
		APIKey:   firstNonEmpty(apiKey, s.cfg.AIAPIKey),
		Model:    model,
		Enabled:  kv[keyEnabled] == "1",
	}
	s.cached = st
	s.cachedAt = time.Now()
	return st, nil
}

func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// SaveSettings menyimpan konfigurasi dari dashboard.
func (s *Service) SaveSettings(ctx context.Context, st Settings) error {
	if p := ProviderByKey(st.Provider); p != nil && st.BaseURL == "" {
		st.BaseURL = p.BaseURL
	}
	key := st.APIKey
	if s.cipher != nil && key != "" {
		if k, err := s.cipher.Encrypt(key); err == nil {
			key = k
		}
	}
	vals := map[string]string{
		keyProvider: st.Provider,
		keyBaseURL:  st.BaseURL,
		keyAPIKey:   key,
		keyModel:    st.Model,
		keyEnabled:  "0",
	}
	if st.Enabled {
		vals[keyEnabled] = "1"
	}
	for k, v := range vals {
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	s.InvalidateCache()
	return nil
}

// Answer menjawab pertanyaan pelanggan dengan konteks dari knowledge base
// dan riwayat percakapan (memori). Mengembalikan ("", nil) jika AI
// nonaktif/bermasalah agar pemanggil bisa jatuh ke balasan default.
func (s *Service) Answer(ctx context.Context, question string, history []store.ChatMessage) (string, error) {
	st, err := s.Settings(ctx)
	if err != nil || !st.Enabled || st.APIKey == "" {
		return "", nil
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return "", nil
	}

	// Cache pertanyaan identik (normalisasi huruf kecil) agar tidak
	// memanggil API berulang untuk pertanyaan yang sama.
	cacheKey := strings.ToLower(question)
	if cached, ok := s.cacheGet(cacheKey); ok {
		return cached, nil
	}

	maxTokens := s.cfg.AIMaxTokens
	if s.stg != nil {
		if st, err := s.stg.Get(ctx); err == nil && st.AIMaxTokens > 0 {
			maxTokens = st.AIMaxTokens
		}
	}
	client := NewClient(st.BaseURL, st.APIKey, st.Model, maxTokens)

	// Retrieval memakai pertanyaan + pesan terakhir sebelum pertanyaan ini,
	// agar pertanyaan kontekstual ("yang tadi berapa?") tetap menemukan
	// data produk yang sedang dibicarakan (baik dari pesan customer
	// maupun balasan bot).
	retrievalQuery := question
	if len(history) >= 2 {
		prev := history[len(history)-2]
		if body := strings.TrimSpace(prev.Body); body != "" {
			retrievalQuery += " " + body
		}
	}

	chunks, err := s.store.SearchKnowledgeChunks(ctx, retrievalQuery, 5)
	if err != nil {
		log.Printf("ai: search knowledge: %v", err)
	}
	kbContext := strings.Join(chunks, "\n\n---\n\n")

	// Data produk dari dashboard (admin/products) ikut dijadikan konteks,
	// sehingga AI bisa menjawab harga/stok/produk dari data terbaru DB.
	products, err := s.store.ListProducts(ctx, true)
	if err != nil {
		log.Printf("ai: list products: %v", err)
	}
	maxProducts := s.cfg.AIMaxProducts
	if s.stg != nil {
		if st, err := s.stg.Get(ctx); err == nil && st.AIMaxProducts > 0 {
			maxProducts = st.AIMaxProducts
		}
	}
	prodContext := productContext(products, retrievalQuery, maxProducts)

	var contextParts []string
	if prodContext != "" {
		contextParts = append(contextParts, prodContext)
	}
	if kbContext != "" {
		contextParts = append(contextParts, "KNOWLEDGE BASE (dokumen yang diunggah):\n"+kbContext)
	}
	context := strings.Join(contextParts, "\n\n---\n\n")

	storeName, storeAddress, adminPhone, aiPersonality, aiName, storeHours, paymentMethods := s.cfg.StoreName, "", "", "", "", "", ""
	if s.stg != nil {
		if st, err := s.stg.Get(ctx); err == nil {
			if st.StoreName != "" {
				storeName = st.StoreName
			}
			storeAddress = st.StoreAddress
			adminPhone = st.AdminPhone
			aiPersonality = st.AIPersonality
			aiName = st.AIName
			storeHours = st.StoreHours
			paymentMethods = st.PaymentMethods
		}
	}
	system := systemPrompt(storeName, storeAddress, adminPhone, aiPersonality, aiName, storeHours, paymentMethods, len(products) > 0 || len(chunks) > 0)
	prompt := promptFor(question, context, history)

	answer, err := client.Chat(ctx, system, prompt)
	if err != nil {
		log.Printf("ai: chat gagal: %v", err)
		return "", err
	}
	if answer != "" {
		s.cachePut(cacheKey, answer)
	}
	return answer, nil
}

func (s *Service) cacheGet(key string) (string, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	e, ok := s.cache[key]
	if !ok || time.Since(e.cachedAt) > cacheTTL {
		return "", false
	}
	return e.answer, true
}

func (s *Service) cachePut(key, answer string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cache) > 500 {
		s.cache = map[string]cacheEntry{}
	}
	s.cache[key] = cacheEntry{answer: answer, cachedAt: time.Now()}
}

func systemPrompt(storeName, storeAddress, adminPhone, aiPersonality, aiName, storeHours, paymentMethods string, hasData bool) string {
	var info strings.Builder
	if aiName != "" {
		fmt.Fprintf(&info, "Namamu adalah %s, asisten layanan pelanggan WhatsApp untuk toko \"%s\". ", aiName, storeName)
	} else {
		fmt.Fprintf(&info, "Kamu adalah asisten layanan pelanggan WhatsApp untuk toko \"%s\". ", storeName)
	}
	if aiPersonality != "" {
		fmt.Fprintf(&info, "Kepribadianmu: %s. Ikuti kepribadian ini saat menjawab pelanggan. ", aiPersonality)
	}
	if storeAddress != "" {
		fmt.Fprintf(&info, "Alamat toko: %s. ", storeAddress)
	}
	if storeHours != "" {
		fmt.Fprintf(&info, "Jam operasional: %s. ", storeHours)
	}
	if paymentMethods != "" {
		fmt.Fprintf(&info, "Metode pembayaran: %s. ", paymentMethods)
	}
	if adminPhone != "" {
		fmt.Fprintf(&info, "Kontak admin (nomor WhatsApp): %s. ", adminPhone)
	}
	info.WriteString("Jawab dalam Bahasa Indonesia, singkat, ramah, dan sopan. ")
	if hasData {
		info.WriteString("Jawab pertanyaan pelanggan berdasarkan DATA TOKO yang diberikan. " +
			"Gunakan harga, produk, dan keterangan yang ada di data. " +
			"Jika jawaban tidak ada di data, katakan tidak tahu dan sarankan menghubungi admin. " +
			"Jangan mengarang harga, stok, atau produk.")
	} else {
		info.WriteString("Jawab pertanyaan umum pelanggan dengan singkat dan ramah. " +
			"Jika tidak yakin, arahkan pelanggan untuk menghubungi admin. " +
			"Jangan mengarang informasi spesifik (harga, stok) karena belum ada data toko.")
	}
	return info.String()
}

func promptFor(question, context string, history []store.ChatMessage) string {
	var b strings.Builder

	// Memori: riwayat percakapan terakhir (paling baru di bawah).
	hist := historyText(history, 8)
	if hist != "" {
		b.WriteString("RIWAYAT PERCAKAPAN (untuk konteks, yang terbaru di bawah):\n")
		b.WriteString(hist)
		b.WriteString("\n\n")
	}

	b.WriteString("PERTANYAAN PELANGGAN:\n")
	b.WriteString(question)
	if context != "" {
		b.WriteString("\n\nDATA TOKO (gunakan untuk menjawab):\n")
		b.WriteString(context)
	}
	return b.String()
}

// historyText menyusun riwayat pesan sebagai "Pelanggan: ..." / "Bot: ...".
// Pesan terakhir (pertanyaan saat ini) diabaikan agar tidak ganda.
func historyText(msgs []store.ChatMessage, max int) string {
	if len(msgs) <= 1 {
		return ""
	}
	msgs = msgs[:len(msgs)-1] // buang pesan terbaru (pertanyaan sekarang)
	if len(msgs) > max {
		msgs = msgs[len(msgs)-max:]
	}
	var b strings.Builder
	for _, m := range msgs {
		body := strings.TrimSpace(m.Body)
		if body == "" || len([]rune(body)) > 300 {
			continue
		}
		switch m.Direction {
		case "in":
			b.WriteString("Pelanggan: ")
		default:
			b.WriteString("Bot: ")
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// productContext menyusun produk aktif dari dashboard (admin/products) menjadi
// teks konteks untuk AI. Hanya produk yang cocok dengan kata kunci pertanyaan
// yang disertakan (hemat token); bila tak ada yang cocok, sejumlah kecil
// produk ditampilkan agar AI tetap punya gambaran toko.
func productContext(products []store.Product, query string, maxShown int) string {
	if len(products) == 0 {
		return ""
	}
	if maxShown < 1 {
		maxShown = 5
	}
	words := queryWords(query)
	type scored struct {
		p     store.Product
		score int
	}
	scoredList := make([]scored, 0, len(products))
	for _, p := range products {
		hay := strings.ToLower(p.Name + " " + p.Description)
		n := 0
		for _, w := range words {
			if w != "" && strings.Contains(hay, w) {
				n++
			}
		}
		scoredList = append(scoredList, scored{p, n})
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// Hanya produk relevan (score > 0), maksimal maxShown.
	relevant := 0
	for _, s := range scoredList {
		if s.score > 0 {
			relevant++
		}
	}
	show := relevant
	if show == 0 {
		show = 5 // fallback: tetap kenalkan katalog walau tak ada yang cocok
	}
	if show > maxShown {
		show = maxShown
	}

	var b strings.Builder
	b.WriteString("PRODUK TOKO (data terbaru dari dashboard admin/products):\n")
	for i, s := range scoredList {
		if i >= show {
			break
		}
		p := s.p
		fmt.Fprintf(&b, "- %s | harga: %s", p.Name, formatPrice(p.Price))
		if p.Stock >= 0 {
			fmt.Fprintf(&b, " | stok: %d", p.Stock)
		} else {
			b.WriteString(" | stok: tersedia")
		}
		if p.Description != "" {
			fmt.Fprintf(&b, " | keterangan: %s", p.Description)
		}
		if p.PurchaseLink != "" {
			fmt.Fprintf(&b, " | link pembelian: %s", p.PurchaseLink)
		}
		b.WriteString("\n")
	}
	if relevant == 0 && len(products) > show {
		fmt.Fprintf(&b, "(katalog toko punya %d produk lain — pelanggan bisa ketik *menu* untuk melihatnya)\n", len(products)-show)
	}
	return b.String()
}

// queryWords memecah pertanyaan menjadi kata kunci (>= 3 huruf).
func queryWords(q string) []string {
	var words []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool {
		return r <= ' ' || r == ',' || r == '.' || r == ';' || r == ':' || r == '?' || r == '!' || r == '"' || r == '\''
	}) {
		w = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]{}-"))
		if len([]rune(w)) >= 3 {
			words = append(words, w)
		}
	}
	return words
}

// formatPrice merender nominal dalam Rupiah dengan pemisah ribuan.
func formatPrice(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp" + b.String()
	if neg {
		out = "-" + out
	}
	return out
}
