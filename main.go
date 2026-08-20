package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/dirman/bot-admin-whatsapp/internal/ai"
	"github.com/dirman/bot-admin-whatsapp/internal/config"
	"github.com/dirman/bot-admin-whatsapp/internal/cryptx"
	"github.com/dirman/bot-admin-whatsapp/internal/dashboard"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/router"
	"github.com/dirman/bot-admin-whatsapp/internal/settings"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// Webhook tanpa secret yang aman tidak boleh berjalan — siapa pun bisa
	// menyuntikkan pesan palsu ke bot.
	if cfg.GowaWebhookSecret == "" || cfg.GowaWebhookSecret == "secret" {
		log.Fatalf("GOWA_WEBHOOK_SECRET wajib diisi dengan nilai rahasia (default \"secret\" tidak diizinkan). Set di .env lalu jalankan ulang.")
	}

	// Kunci enkripsi kredensial di DB diturunkan dari SESSION_SECRET.
	cipher, err := cryptx.New(cfg.SessionSecret)
	if err != nil {
		log.Fatalf("cipher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Database ---
	dsn := cfg.DatabaseURL
	if cfg.DBDriver == "sqlite" {
		dsn = cfg.SQLitePath
	}
	db, err := store.Open(ctx, cfg.DBDriver, dsn)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer db.Close()

	st := store.New(db, cfg.DBDriver)
	st.SetCipher(cipher)
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if cfg.UploadDir != "" {
		if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
			log.Printf("warn: buat direktori upload: %v", err)
		}
	}

	gowa := gowaclient.New(cfg.GowaBaseURL, st)
	stg := settings.New(st, cfg)
	aiSvc := ai.New(st, cfg, stg, cipher)
	rtr := router.New(st, gowa, cfg, aiSvc, stg)

	// --- Dashboard admin ---
	dash := dashboard.New(st, gowa, cfg, rtr, aiSvc, stg)

	// --- Broadcast worker ---
	go broadcastWorker(ctx, st, gowa, cfg, stg)

	// --- HTTP server ---
	webhookSem := make(chan struct{}, 32) // maks 32 webhook diproses bersamaan
	app := fiber.New(fiber.Config{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		BodyLimit:    25 << 20, // 25 MB untuk upload knowledge base
	})
	app.Use(recover.New())
	app.Use(logger.New())

	// Dashboard admin routes.
	dash.Register(app)

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "time": time.Now().Format(time.RFC3339)})
	})

	app.Post("/webhook/gowa", func(c *fiber.Ctx) error {
		body := c.Body()
		// Verifikasi HMAC signature (X-Hub-Signature-256). Wajib selalu.
		sig := c.Get("X-Hub-Signature-256")
		if sig == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing webhook signature"})
		}
		mac := hmac.New(sha256.New, []byte(cfg.GowaWebhookSecret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid webhook signature"})
		}
		// Batasi pemrosesan bersamaan agar flood webhook tidak menumpuk
		// goroutine/DB/API AI.
		select {
		case webhookSem <- struct{}{}:
			go func() {
				defer func() { <-webhookSem }()
				rtr.HandleWebhook(context.Background(), body)
			}()
		default:
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "busy, coba lagi"})
		}
		return c.SendStatus(fiber.StatusOK)
	})

	// --- Graceful shutdown ---
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("shutting down...")
		cancel()
		_ = app.Shutdown()
	}()

	log.Printf("bot-admin-whatsapp listening on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Println("bye.")
}

// broadcastWorker periodically picks up pending broadcasts and sends them
// to the target customers with a delay between each message.
func broadcastWorker(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config, stg *settings.Service) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runPendingBroadcasts(ctx, st, gowa, cfg, stg)
		}
	}
}

func runPendingBroadcasts(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config, stg *settings.Service) {
	broadcasts, err := st.ListBroadcasts(ctx)
	if err != nil {
		log.Printf("broadcast: list: %v", err)
		return
	}
	for _, b := range broadcasts {
		if b.Status != "pending" {
			continue
		}
		go sendBroadcast(ctx, st, gowa, cfg, stg, b)
	}
}

// adminPhone mengambil nomor admin dari pengaturan toko; jatuh ke .env saat gagal.
func adminPhone(ctx context.Context, stg *settings.Service, cfg *config.Config) string {
	if stg != nil {
		if st, err := stg.Get(ctx); err == nil && st.AdminPhone != "" {
			return st.AdminPhone
		}
	}
	return cfg.AdminPhone
}

func sendBroadcast(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config, stg *settings.Service, b store.Broadcast) {
	if err := st.SetBroadcastRunning(ctx, b.ID); err != nil {
		log.Printf("broadcast %d: mark running: %v", b.ID, err)
		return
	}

	customers, err := st.ListCustomers(ctx, "")
	if err != nil {
		log.Printf("broadcast %d: list customers: %v", b.ID, err)
		_ = st.FinishBroadcast(ctx, b.ID, "failed")
		return
	}

	var sent, failed int
	for _, c := range customers {
		if ctx.Err() != nil {
			break
		}
		if c.Status == "blocked" || c.Phone == "" || c.Phone == adminPhone(ctx, stg, cfg) {
			continue
		}
		// LID-based customers have no exposed phone number; use the full JID.
		target := c.Phone
		if strings.HasSuffix(c.JID, "@lid") {
			target = c.JID
		}
		if _, err := gowa.SendText(ctx, target, b.Message); err != nil {
			failed++
		} else {
			sent++
		}
		if err := st.BroadcastProgress(ctx, b.ID, sent, failed); err != nil {
			log.Printf("broadcast %d: progress: %v", b.ID, err)
		}
		select {
		case <-ctx.Done():
			_ = st.FinishBroadcast(ctx, b.ID, "failed")
			return
		case <-time.After(cfg.BroadcastDelay):
		}
	}

	status := "done"
	if failed > 0 && sent == 0 {
		status = "failed"
	}
	_ = st.FinishBroadcast(ctx, b.ID, status)
	log.Printf("broadcast %d selesai: sent=%d failed=%d", b.ID, sent, failed)
}
