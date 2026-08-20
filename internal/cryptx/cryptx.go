// Package cryptx menyediakan enkripsi AES-GCM untuk kredensial yang
// disimpan di database (password/token gowa, API key AI).
package cryptx

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const prefix = "enc:"

// Cipher membungkus kunci AES-256 yang diturunkan dari secret (SESSION_SECRET).
type Cipher struct {
	gcm cipher.AEAD
}

// New membangun Cipher dari secret. Secret wajib tidak kosong.
func New(secret string) (*Cipher, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("cryptx: secret kosong")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("cryptx: buat cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cryptx: buat gcm: %w", err)
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt mengenkripsi plaintext menjadi "enc:<base64(nonce||ciphertext)>".
func (c *Cipher) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := c.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt mendekripsi nilai ber-prefix "enc:". Nilai tanpa prefix dianggap
// data lama (plaintext) dan dikembalikan apa adanya.
func (c *Cipher) Decrypt(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if !strings.HasPrefix(v, prefix) {
		return v, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, prefix))
	if err != nil {
		return "", err
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("cryptx: ciphertext terlalu pendek")
	}
	plain, err := c.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
