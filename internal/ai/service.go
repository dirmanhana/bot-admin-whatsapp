package ai

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/config"
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
	store *store.Store
	cfg   *config.Config

	mu       sync.Mutex
	cached   *Settings
	cachedAt time.Time
}

func New(st *store.Store, cfg *config.Config) *Service {
	return &Service{store: st, cfg: cfg}
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
	st := &Settings{
		Provider: prov,
		BaseURL:  baseURL,
		APIKey:   firstNonEmpty(kv[keyAPIKey], s.cfg.AIAPIKey),
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
	vals := map[string]string{
		keyProvider: st.Provider,
		keyBaseURL:  st.BaseURL,
		keyAPIKey:   st.APIKey,
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

	client := NewClient(st.BaseURL, st.APIKey, st.Model)

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
	context := strings.Join(chunks, "\n\n---\n\n")

	system := systemPrompt(s.cfg.StoreName, len(chunks) > 0)
	prompt := promptFor(question, context, history)

	answer, err := client.Chat(ctx, system, prompt)
	if err != nil {
		log.Printf("ai: chat gagal: %v", err)
		return "", err
	}
	return answer, nil
}

func systemPrompt(storeName string, hasData bool) string {
	if hasData {
		return "Kamu adalah asisten layanan pelanggan WhatsApp untuk toko \"" + storeName + "\". " +
			"Jawab pertanyaan pelanggan berdasarkan DATA TOKO yang diberikan. " +
			"Gunakan harga, produk, dan keterangan yang ada di data. " +
			"Jika jawaban tidak ada di data, katakan tidak tahu dan sarankan menghubungi admin. " +
			"Jangan mengarang harga, stok, atau produk. Jawab dalam Bahasa Indonesia, singkat, ramah, dan sopan."
	}
	return "Kamu adalah asisten layanan pelanggan WhatsApp untuk toko \"" + storeName + "\". " +
		"Jawab pertanyaan umum pelanggan dengan singkat, ramah, dalam Bahasa Indonesia. " +
		"Jika tidak yakin, arahkan pelanggan untuk menghubungi admin. " +
		"Jangan mengarang informasi spesifik (harga, stok) karena belum ada data toko."
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
