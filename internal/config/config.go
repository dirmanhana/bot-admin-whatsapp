package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port              string
	DatabaseURL       string
	GowaBaseURL       string
	GowaWebhookURL    string
	GowaWebhookSecret string
	AdminPhone        string
	DashboardUser     string
	DashboardPassword string
	SessionSecret     string
	BroadcastDelay    time.Duration
	StoreName         string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://dirman@127.0.0.1:5433/bot_admin_whatsapp?sslmode=disable"),
		GowaBaseURL:       getEnv("GOWA_BASE_URL", "http://127.0.0.1:3000"),
		GowaWebhookURL:    getEnv("GOWA_WEBHOOK_URL", ""),
		GowaWebhookSecret: getEnv("GOWA_WEBHOOK_SECRET", "secret"),
		AdminPhone:        getEnv("ADMIN_PHONE", ""),
		DashboardUser:     getEnv("DASHBOARD_USER", "admin"),
		DashboardPassword: getEnv("DASHBOARD_PASSWORD", "admin123"),
		SessionSecret:     getEnv("SESSION_SECRET", "insecure-session-secret"),
		StoreName:         getEnv("STORE_NAME", "Toko Kita"),
	}

	delaySec, err := strconv.Atoi(getEnv("BROADCAST_DELAY_SECONDS", "7"))
	if err != nil || delaySec < 1 {
		delaySec = 7
	}
	cfg.BroadcastDelay = time.Duration(delaySec) * time.Second

	if cfg.SessionSecret == "insecure-session-secret" || cfg.SessionSecret == "ubah-saya-session-secret-sangat-rahasia" {
		fmt.Println("[warn] SESSION_SECRET masih default. Ganti di .env untuk produksi.")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
