package gowaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

const DeviceIDHeader = "X-Device-Id"

type Client struct {
	baseURL string
	http    *http.Client
	store   *store.Store
	mu      sync.Mutex
}

type Response struct {
	Status  int             `json:"status"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Results json.RawMessage `json:"results"`
}

func New(baseURL string, st *store.Store) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
		store:   st,
	}
}

// Login authenticates to gowa and returns the issued token and expiry.
func (c *Client) Login(ctx context.Context, username, password string) (token string, expires time.Time, err error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := c.doRaw(ctx, http.MethodPost, "/auth/login", body, "")
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("gowa login gagal (status %d): %s", resp.StatusCode, string(b))
	}

	var out struct {
		Results struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("decode login response: %w", err)
	}
	return out.Results.Token, out.Results.ExpiresAt, nil
}

// EnsureToken returns a valid bearer token for the active account, logging in
// (and persisting the token) when missing or near expiry.
func (c *Client) EnsureToken(ctx context.Context) (token, deviceID string, err error) {
	acc, err := c.store.GetActiveWAAccount(ctx)
	if err != nil {
		return "", "", err
	}
	if acc == nil {
		return "", "", fmt.Errorf("belum ada akun gowa aktif. Tambahkan di Dashboard > Pengaturan")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	valid := acc.Token != "" && acc.TokenExpiresAt != nil && now.Before(acc.TokenExpiresAt.Add(-5*time.Minute))
	if !valid {
		tok, exp, err := c.Login(ctx, acc.Username, acc.Password)
		if err != nil {
			return "", "", err
		}
		if err := c.store.UpdateWAToken(ctx, acc.ID, tok, exp); err != nil {
			return "", "", fmt.Errorf("simpan token: %w", err)
		}
		token = tok
	} else {
		token = acc.Token
	}
	return token, acc.DeviceID, nil
}

// Ping authenticates with the given credentials and returns the active device's id.
func (c *Client) Ping(ctx context.Context, username, password string) (string, error) {
	token, _, err := c.Login(ctx, username, password)
	if err != nil {
		return "", err
	}
	var devs []struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(ctx, token, "", "/devices", &devs); err != nil {
		return "", err
	}
	if len(devs) == 0 {
		return "", nil
	}
	return devs[0].ID, nil
}

func (c *Client) ListDevices(ctx context.Context) ([]map[string]any, error) {
	token, deviceID, err := c.EnsureToken(ctx)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := c.getJSON(ctx, token, deviceID, "/devices", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateDevice(ctx context.Context, deviceID string) (map[string]any, error) {
	token, _, err := c.EnsureToken(ctx)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]string{"device_id": deviceID})
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, token, "", "/devices", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) LoginDeviceQR(ctx context.Context, deviceID string) (string, time.Duration, error) {
	token, _, err := c.EnsureToken(ctx)
	if err != nil {
		return "", 0, err
	}
	var out struct {
		QRLink     string `json:"qr_link"`
		QRDuration int    `json:"qr_duration"`
	}
	path := "/devices/" + deviceID + "/login"
	if err := c.doJSON(ctx, http.MethodGet, token, "", path, nil, &out); err != nil {
		return "", 0, err
	}
	return out.QRLink, time.Duration(out.QRDuration) * time.Second, nil
}

func (c *Client) DeviceStatus(ctx context.Context, deviceID string) (connected, loggedIn bool, err error) {
	token, _, err := c.EnsureToken(ctx)
	if err != nil {
		return false, false, err
	}
	var out struct {
		IsConnected bool `json:"is_connected"`
		IsLoggedIn  bool `json:"is_logged_in"`
	}
	path := "/devices/" + deviceID + "/status"
	if err := c.doJSON(ctx, http.MethodGet, token, "", path, nil, &out); err != nil {
		return false, false, err
	}
	return out.IsConnected, out.IsLoggedIn, nil
}

func (c *Client) SetDeviceWebhook(ctx context.Context, deviceID, webhookURL, secret string) error {
	token, _, err := c.EnsureToken(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"webhook_url":    webhookURL,
		"webhook_secret": secret,
		"webhook_events": "message,message.ack",
	})
	var out map[string]any
	path := "/devices/" + deviceID + "/webhook"
	if err := c.doJSON(ctx, http.MethodPatch, token, "", path, body, &out); err != nil {
		return err
	}
	return nil
}

// SendText sends a plain text message to a phone number.
func (c *Client) SendText(ctx context.Context, phone, message string) (string, error) {
	token, deviceID, err := c.EnsureToken(ctx)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]string{"phone": phone, "message": message})
	var out struct {
		MessageID string `json:"message_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, token, deviceID, "/send/message", body, &out); err != nil {
		return "", err
	}
	return out.MessageID, nil
}

// SendImage sends an image with caption from a local file path.
func (c *Client) SendImage(ctx context.Context, phone, imagePath, caption string) (string, error) {
	token, deviceID, err := c.EnsureToken(ctx)
	if err != nil {
		return "", err
	}

	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("phone", phone)
	_ = mw.WriteField("caption", caption)
	fw, err := mw.CreateFormFile("image", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return "", err
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/send/image", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(DeviceIDHeader, deviceID)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gowa send/image gagal (status %d): %s", resp.StatusCode, string(b))
	}
	var wrapper Response
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return "", err
	}
	var out struct {
		MessageID string `json:"message_id"`
	}
	if wrapper.Results != nil {
		if err := json.Unmarshal(wrapper.Results, &out); err != nil {
			return "", err
		}
	}
	return out.MessageID, nil
}

// ---------- helpers ----------

func (c *Client) doRaw(ctx context.Context, method, path string, body []byte, token string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.http.Do(req)
}

func (c *Client) getJSON(ctx context.Context, token, deviceID, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if deviceID != "" {
		req.Header.Set(DeviceIDHeader, deviceID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gowa GET %s gagal (status %d): %s", path, resp.StatusCode, string(b))
	}
	var wrapper Response
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if wrapper.Results != nil && out != nil {
		if err := json.Unmarshal(wrapper.Results, out); err != nil {
			return fmt.Errorf("decode results %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, token, deviceID, path string, body []byte, out any) error {
	resp, err := c.doRaw(ctx, method, path, body, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gowa %s %s gagal (status %d): %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		var wrapper Response
		if err := json.Unmarshal(b, &wrapper); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if wrapper.Results != nil {
			if err := json.Unmarshal(wrapper.Results, out); err != nil {
				return fmt.Errorf("decode results %s: %w", path, err)
			}
		}
	}
	return nil
}
