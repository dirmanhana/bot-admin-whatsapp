package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	DatabaseURL         string
	GowaBaseURL         string
	GowaWebhookURL      string
	GowaWebhookSecret   string
	AdminPhone          string
	DashboardUser       string
	DashboardPassword   string
	SessionSecret       string
	BroadcastDelay      time.Duration
	StoreName           string
	AIProvider          string
	AIAPIKey            string
	AIBaseURL           string
	AIModel             string
	LoginMaxAttempts    int
	LoginLockoutMinutes int
	LogRedact           bool
	AllowRegistration   bool
	AIDailyQuota        int
	AIMaxTokens         int
	AIMaxProducts       int
	AIMaxHistory        int
	UploadDir           string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://dirman@127.0.0.1:5433/bot_admin_whatsapp?sslmode=disable"),
		GowaBaseURL:         getEnv("GOWA_BASE_URL", "http://127.0.0.1:3000"),
		GowaWebhookURL:      getEnv("GOWA_WEBHOOK_URL", ""),
		GowaWebhookSecret:   getEnv("GOWA_WEBHOOK_SECRET", "secret"),
		AdminPhone:          getEnv("ADMIN_PHONE", ""),
		DashboardUser:       getEnv("DASHBOARD_USER", "admin"),
		DashboardPassword:   getEnv("DASHBOARD_PASSWORD", "admin123"),
		SessionSecret:       getEnv("SESSION_SECRET", "insecure-session-secret"),
		StoreName:           getEnv("STORE_NAME", "Toko Kita"),
		AIProvider:          getEnv("AI_PROVIDER", ""),
		AIAPIKey:            getEnv("AI_API_KEY", ""),
		AIBaseURL:           getEnv("AI_BASE_URL", ""),
		AIModel:             getEnv("AI_MODEL", ""),
		LoginMaxAttempts:    getEnvInt("LOGIN_MAX_ATTEMPTS", 5),
		LoginLockoutMinutes: getEnvInt("LOGIN_LOCKOUT_MINUTES", 15),
		LogRedact:           getEnv("LOG_REDACT", "true") != "false",
		AllowRegistration:   getEnv("ALLOW_REGISTRATION", "true") != "false",
		AIDailyQuota:        getEnvInt("AI_DAILY_QUOTA", 20),
		AIMaxTokens:         getEnvInt("AI_MAX_TOKENS", 600),
		AIMaxProducts:       getEnvInt("AI_MAX_PRODUCTS", 15),
		AIMaxHistory:        getEnvInt("AI_MAX_HISTORY", 15),
		UploadDir:           getEnv("UPLOAD_DIR", "data/products"),
	}

	delaySec, err := strconv.Atoi(getEnv("BROADCAST_DELAY_SECONDS", "7"))
	if err != nil || delaySec < 1 {
		delaySec = 7
	}
	cfg.BroadcastDelay = time.Duration(delaySec) * time.Second

	if cfg.SessionSecret == "insecure-session-secret" || cfg.SessionSecret == "ubah-saya-session-secret-sangat-rahasia" {
		fmt.Println("[warn] SESSION_SECRET masih default. Ganti di .env untuk produksi.")
	}
	if cfg.GowaWebhookSecret == "secret" {
		fmt.Println("[warn] GOWA_WEBHOOK_SECRET masih default. Aplikasi akan menolak berjalan sampai diubah.")
	}
	return cfg, nil
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
