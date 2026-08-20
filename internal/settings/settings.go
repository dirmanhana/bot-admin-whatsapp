package settings

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

const (
	KeyStoreName      = "store_name"
	KeyStoreAddr      = "store_address"
	KeyAdminPhone     = "admin_phone"
	KeyAIPersonality  = "ai_personality"
	KeyAIName         = "ai_name"
	KeyStoreHours     = "store_hours"
	KeyPaymentMethods = "payment_methods"
	KeyDeliveryFee    = "delivery_fee"
	KeyAIDailyQuota   = "ai_daily_quota"
	KeyAIMaxTokens    = "ai_max_tokens"
	KeyAIMaxProducts  = "ai_max_products"
	KeyAIMaxHistory   = "ai_max_history"
	KeyDashPassword   = "dashboard_password_hash"
	KeyDashSessionEpoch = "dashboard_session_epoch"
)

// Settings adalah pengaturan toko yang disimpan di tabel settings.
type Settings struct {
	StoreName      string
	StoreAddress   string
	AdminPhone     string
	AIPersonality  string
	AIName         string
	StoreHours     string
	PaymentMethods string
	DeliveryFee    int64
	AIDailyQuota   int // 0 = tanpa batas
	AIMaxTokens    int
	AIMaxProducts  int // maks produk di konteks AI
	AIMaxHistory   int // maks pesan riwayat percakapan
}

// Service menyediakan pengaturan toko dengan cache singkat agar tidak
// membebani DB per pesan. Nilai dari database menang atas .env.
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

// Get memuat pengaturan toko; nilai dari database menang atas .env.
// Di-cache 60 detik.
func (s *Service) Get(ctx context.Context) (*Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil && time.Since(s.cachedAt) < 60*time.Second {
		return s.cached, nil
	}
	kv, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	st := &Settings{
		StoreName:      firstNonEmpty(kv[KeyStoreName], s.cfg.StoreName),
		StoreAddress:   strings.TrimSpace(kv[KeyStoreAddr]),
		AdminPhone:     firstNonEmpty(kv[KeyAdminPhone], s.cfg.AdminPhone),
		AIPersonality:  strings.TrimSpace(kv[KeyAIPersonality]),
		AIName:         strings.TrimSpace(kv[KeyAIName]),
		StoreHours:     strings.TrimSpace(kv[KeyStoreHours]),
		PaymentMethods: strings.TrimSpace(kv[KeyPaymentMethods]),
		AIDailyQuota:   s.cfg.AIDailyQuota,
		AIMaxTokens:    s.cfg.AIMaxTokens,
		AIMaxProducts:  s.cfg.AIMaxProducts,
		AIMaxHistory:   s.cfg.AIMaxHistory,
	}
	if v := strings.TrimSpace(kv[KeyDeliveryFee]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			st.DeliveryFee = n
		}
	}
	if v := strings.TrimSpace(kv[KeyAIDailyQuota]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			st.AIDailyQuota = n
		}
	}
	if v := strings.TrimSpace(kv[KeyAIMaxTokens]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			st.AIMaxTokens = n
		}
	}
	if v := strings.TrimSpace(kv[KeyAIMaxProducts]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			st.AIMaxProducts = n
		}
	}
	if v := strings.TrimSpace(kv[KeyAIMaxHistory]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			st.AIMaxHistory = n
		}
	}
	s.cached = st
	s.cachedAt = time.Now()
	return st, nil
}

// Save menyimpan pengaturan dari dashboard.
func (s *Service) Save(ctx context.Context, st Settings) error {
	vals := map[string]string{
		KeyStoreName:      strings.TrimSpace(st.StoreName),
		KeyStoreAddr:      strings.TrimSpace(st.StoreAddress),
		KeyAdminPhone:     strings.TrimSpace(st.AdminPhone),
		KeyAIPersonality:  strings.TrimSpace(st.AIPersonality),
		KeyAIName:         strings.TrimSpace(st.AIName),
		KeyStoreHours:     strings.TrimSpace(st.StoreHours),
		KeyPaymentMethods: strings.TrimSpace(st.PaymentMethods),
		KeyDeliveryFee:    strconv.FormatInt(st.DeliveryFee, 10),
		KeyAIDailyQuota:   strconv.Itoa(st.AIDailyQuota),
		KeyAIMaxTokens:    strconv.Itoa(st.AIMaxTokens),
		KeyAIMaxProducts:  strconv.Itoa(st.AIMaxProducts),
		KeyAIMaxHistory:   strconv.Itoa(st.AIMaxHistory),
	}
	for k, v := range vals {
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	s.InvalidateCache()
	return nil
}

func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cached = nil
	s.mu.Unlock()
}

// SessionEpoch membaca epoch sesi langsung dari DB (tanpa cache) agar
// invalidasi sesi berlaku seketika. Default 0 bila belum pernah disetel.
func (s *Service) SessionEpoch(ctx context.Context) int64 {
	kv, err := s.store.GetSettings(ctx)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(kv[KeyDashSessionEpoch], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// BumpSessionEpoch menaikkan epoch sesi; semua sesi lama menjadi tidak valid.
func (s *Service) BumpSessionEpoch(ctx context.Context) error {
	epoch := s.SessionEpoch(ctx) + 1
	if err := s.store.SetSetting(ctx, KeyDashSessionEpoch, strconv.FormatInt(epoch, 10)); err != nil {
		return err
	}
	s.InvalidateCache()
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}