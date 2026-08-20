package dashboard

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/dirman/bot-admin-whatsapp/internal/ai"
	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

const maxUploadSize = 25 << 20 // 25 MB

func (s *Server) pageAI(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
	settings, err := s.ai.Settings(ctx)
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal memuat pengaturan: "+err.Error(), true)
	}
	docs, err := s.store.ListKnowledgeDocs(ctx)
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal memuat knowledge base: "+err.Error(), true)
	}
	return s.render(c, "ai", view{
		Title: "AI & Data", Active: "ai",
		Data: map[string]any{
			"Settings":  settings,
			"Providers": ai.Providers,
			"Docs":      docs,
		},
	})
}

func (s *Server) actionAISave(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
	st := ai.Settings{
		Provider: strings.TrimSpace(c.FormValue("provider")),
		BaseURL:  strings.TrimSpace(c.FormValue("base_url")),
		APIKey:   strings.TrimSpace(c.FormValue("api_key")),
		Model:    strings.TrimSpace(c.FormValue("model")),
		Enabled:  c.FormValue("enabled") == "1" || c.FormValue("enabled") == "on",
	}
	if st.Provider == "" {
		return redirect(c, "/admin/ai", "Pilih penyedia AI.", true)
	}
	if err := s.ai.SaveSettings(ctx, st); err != nil {
		return redirect(c, "/admin/ai", "Gagal menyimpan: "+err.Error(), true)
	}
	return redirect(c, "/admin/ai", "Pengaturan AI disimpan.", false)
}

func (s *Server) actionAITest(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(s.tenantCtx(c), time.Minute)
	defer cancel()

	st, err := s.ai.Settings(ctx)
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal memuat pengaturan: "+err.Error(), true)
	}
	if st.APIKey == "" {
		return redirect(c, "/admin/ai", "API key belum diisi.", true)
	}
	maxTokens := s.cfg.AIMaxTokens
	if stt, err := s.settings.Get(ctx); err == nil && stt.AIMaxTokens > 0 {
		maxTokens = stt.AIMaxTokens
	}
	client := ai.NewClient(st.BaseURL, st.APIKey, st.Model, maxTokens)
	if err := client.Test(ctx); err != nil {
		return redirect(c, "/admin/ai", "Tes gagal: "+err.Error(), true)
	}
	return redirect(c, "/admin/ai", "Koneksi AI berhasil ✓", false)
}

func (s *Server) actionKnowledgeUpload(c *fiber.Ctx) error {
	ctx := s.tenantCtx(c)
	file, err := c.FormFile("file")
	if err != nil {
		return redirect(c, "/admin/ai", "Pilih file terlebih dahulu.", true)
	}
	if !ai.AllowedExts[strings.ToLower(filepath.Ext(file.Filename))] {
		return redirect(c, "/admin/ai", "Format tidak didukung (dukung: PDF, CSV, XLSX, TXT).", true)
	}
	if file.Size > maxUploadSize {
		return redirect(c, "/admin/ai", "File terlalu besar (maks 25 MB).", true)
	}

	src, err := file.Open()
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal membaca file.", true)
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal membaca file: "+err.Error(), true)
	}

	text, err := ai.ExtractText(file.Filename, data)
	if err != nil {
		return redirect(c, "/admin/ai", err.Error(), true)
	}
	if strings.TrimSpace(text) == "" {
		return redirect(c, "/admin/ai", "Tidak ada teks yang bisa diekstrak dari file ini.", true)
	}
	chunks := ai.ChunkText(text)
	if len(chunks) == 0 {
		return redirect(c, "/admin/ai", "Tidak ada konten yang bisa diindeks.", true)
	}

	docID, err := s.store.AddKnowledgeDoc(ctx, &store.KnowledgeDoc{
		Filename:   file.Filename,
		FileType:   strings.TrimPrefix(strings.ToLower(filepath.Ext(file.Filename)), "."),
		SizeBytes:  int64(len(data)),
		ChunkCount: len(chunks),
	}, data)
	if err != nil {
		return redirect(c, "/admin/ai", "Gagal menyimpan dokumen: "+err.Error(), true)
	}
	if err := s.store.AddKnowledgeChunks(ctx, docID, chunks); err != nil {
		return redirect(c, "/admin/ai", "Gagal mengindeks dokumen: "+err.Error(), true)
	}
	return redirect(c, "/admin/ai", "Dokumen berhasil diunggah ("+strconv.Itoa(len(chunks))+" bagian).", false)
}

func (s *Server) actionKnowledgeDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("id tidak valid")
	}
	if err := s.store.DeleteKnowledgeDoc(s.tenantCtx(c), id); err != nil {
		return redirect(c, "/admin/ai", "Gagal menghapus: "+err.Error(), true)
	}
	return redirect(c, "/admin/ai", "Dokumen dihapus.", false)
}
