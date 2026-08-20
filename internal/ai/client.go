package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider adalah preset penyedia LLM yang mendukung API OpenAI-compatible
// (chat/completions). NVIDIA NIM, DeepSeek, Groq, dan Gemini semuanya
// menyediakan endpoint ini.
type Provider struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	NeedsModel bool   `json:"needs_model"` // model wajib diisi
}

var Providers = []Provider{
	{Key: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat", NeedsModel: true},
	{Key: "groq", Label: "Groq", BaseURL: "https://api.groq.com/openai/v1", Model: "llama-3.3-70b-versatile", NeedsModel: true},
	{Key: "nim", Label: "NVIDIA NIM", BaseURL: "https://integrate.api.nvidia.com/v1", Model: "meta/llama-3.3-70b-instruct", NeedsModel: true},
	{Key: "gemini", Label: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.0-flash", NeedsModel: true},
	{Key: "custom", Label: "Custom (OpenAI-compatible)", BaseURL: "", Model: "", NeedsModel: true},
}

func ProviderByKey(key string) *Provider {
	for i := range Providers {
		if Providers[i].Key == key {
			return &Providers[i]
		}
	}
	return nil
}

// Client berbicara ke endpoint /chat/completions gaya OpenAI.
type Client struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
	http      *http.Client
}

func NewClient(baseURL, apiKey, model string, maxTokens int) *Client {
	if maxTokens <= 0 {
		maxTokens = 600
	}
	return &Client{
		BaseURL:   strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		APIKey:    strings.TrimSpace(apiKey),
		Model:     strings.TrimSpace(model),
		MaxTokens: maxTokens,
		http:      &http.Client{Timeout: 90 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat mengirim satu percakapan (system + user) dan mengembalikan balasan teks.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	if c.BaseURL == "" || c.APIKey == "" || c.Model == "" {
		return "", fmt.Errorf("konfigurasi AI belum lengkap (base URL / API key / model)")
	}
	payload, _ := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"temperature": 0.3,
		"max_tokens":  c.MaxTokens,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request AI gagal: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI menolak (status %d): %s", resp.StatusCode, truncate(string(body), 300))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode respons AI: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("AI error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("AI tidak mengembalikan jawaban")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// Test mengirim prompt kecil untuk memastikan koneksi berhasil.
func (c *Client) Test(ctx context.Context) error {
	_, err := c.Chat(ctx, "Kamu adalah asisten.", "Balas hanya dengan satu kata: OK")
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
