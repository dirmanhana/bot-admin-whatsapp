package dashboard

import (
	"context"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"

	"github.com/dirman/bot-admin-whatsapp/internal/settings"
)

func (s *Server) pageSettings(c *fiber.Ctx) error {
	st, err := s.settings.Get(context.Background())
	if err != nil {
		return redirect(c, "/admin/settings", "Gagal memuat pengaturan: "+err.Error(), true)
	}
	return s.render(c, "settings", view{
		Title: "Pengaturan", Active: "settings",
		Data: map[string]any{"Settings": st},
	})
}

func (s *Server) actionSettingsSave(c *fiber.Ctx) error {
	ctx := context.Background()
	fee, _ := strconv.ParseInt(strings.TrimSpace(c.FormValue("delivery_fee")), 10, 64)
	if fee < 0 {
		fee = 0
	}
	quota, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("ai_daily_quota")))
	if quota < 0 {
		quota = 0
	}
	maxTokens, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("ai_max_tokens")))
	if maxTokens < 1 {
		maxTokens = 600
	}
	maxProducts, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("ai_max_products")))
	if maxProducts < 1 {
		maxProducts = 15
	}
	maxHistory, _ := strconv.Atoi(strings.TrimSpace(c.FormValue("ai_max_history")))
	if maxHistory < 1 {
		maxHistory = 15
	}
	st := settings.Settings{
		StoreName:      strings.TrimSpace(c.FormValue("store_name")),
		StoreAddress:   strings.TrimSpace(c.FormValue("store_address")),
		AdminPhone:     strings.TrimSpace(c.FormValue("admin_phone")),
		AIPersonality:  strings.TrimSpace(c.FormValue("ai_personality")),
		AIName:         strings.TrimSpace(c.FormValue("ai_name")),
		StoreHours:     strings.TrimSpace(c.FormValue("store_hours")),
		PaymentMethods: strings.TrimSpace(c.FormValue("payment_methods")),
		DeliveryFee:    fee,
		AIDailyQuota:   quota,
		AIMaxTokens:    maxTokens,
		AIMaxProducts:  maxProducts,
		AIMaxHistory:   maxHistory,
	}
	if st.StoreName == "" {
		return redirect(c, "/admin/settings", "Nama toko wajib diisi.", true)
	}
	if err := s.settings.Save(ctx, st); err != nil {
		return redirect(c, "/admin/settings", "Gagal menyimpan pengaturan: "+err.Error(), true)
	}

	msg := "Pengaturan disimpan."
	if newPw := c.FormValue("new_password"); newPw != "" {
		cur := c.FormValue("current_password")
		if !s.verifyPassword(ctx, cur) {
			return redirect(c, "/admin/settings", "Password saat ini salah. Password tidak diubah.", true)
		}
		if len(newPw) < 8 {
			return redirect(c, "/admin/settings", "Password baru minimal 8 karakter.", true)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
		if err != nil {
			return redirect(c, "/admin/settings", "Gagal mengenkripsi password: "+err.Error(), true)
		}
		if err := s.store.SetSetting(ctx, settings.KeyDashPassword, string(hash)); err != nil {
			return redirect(c, "/admin/settings", "Gagal menyimpan password: "+err.Error(), true)
		}
		if err := s.settings.BumpSessionEpoch(ctx); err != nil {
			return redirect(c, "/admin/settings", "Password tersimpan, tapi gagal membatalkan sesi lama: "+err.Error(), true)
		}
		msg = "Pengaturan disimpan. Password dashboard diperbarui — silakan masuk ulang."
	}
	return redirect(c, "/admin/settings", msg, false)
}