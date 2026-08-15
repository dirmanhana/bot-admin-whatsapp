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
	"github.com/dirman/bot-admin-whatsapp/internal/dashboard"
	"github.com/dirman/bot-admin-whatsapp/internal/gowaclient"
	"github.com/dirman/bot-admin-whatsapp/internal/router"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Database ---
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if err := store.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	st := store.New(pool)
	gowa := gowaclient.New(cfg.GowaBaseURL, st)
	aiSvc := ai.New(st, cfg)
	rtr := router.New(st, gowa, cfg, aiSvc)

	// --- Dashboard admin ---
	dash := dashboard.New(st, gowa, cfg, rtr, aiSvc)

	// --- Broadcast worker ---
	go broadcastWorker(ctx, st, gowa, cfg)

	// --- HTTP server ---
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
		// Verify HMAC signature (X-Hub-Signature-256) if a secret is configured.
		if cfg.GowaWebhookSecret != "" && cfg.GowaWebhookSecret != "secret" {
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
		}
		// Acknowledge immediately; process asynchronously.
		go rtr.HandleWebhook(context.Background(), body)
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
func broadcastWorker(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runPendingBroadcasts(ctx, st, gowa, cfg)
		}
	}
}

func runPendingBroadcasts(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config) {
	broadcasts, err := st.ListBroadcasts(ctx)
	if err != nil {
		log.Printf("broadcast: list: %v", err)
		return
	}
	for _, b := range broadcasts {
		if b.Status != "pending" {
			continue
		}
		go sendBroadcast(ctx, st, gowa, cfg, b)
	}
}

func sendBroadcast(ctx context.Context, st *store.Store, gowa *gowaclient.Client, cfg *config.Config, b store.Broadcast) {
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
		if c.Status == "blocked" || c.Phone == "" || c.Phone == cfg.AdminPhone {
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
