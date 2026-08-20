package cryptx

import "testing"

func TestEncryptDecrypt(t *testing.T) {
	c, err := New("rahasia-test-123")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plain := "password-gowa-pertama"
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == plain {
		t.Fatal("hasil enkripsi sama dengan plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("decrypt salah: got %q want %q", got, plain)
	}
}

func TestEncryptHasPrefix(t *testing.T) {
	c, _ := New("rahasia-test-123")
	enc, err := c.Encrypt("abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) < len("enc:") || enc[:4] != "enc:" {
		t.Fatalf("nilai terenkripsi tidak ber-prefix enc: -> %q", enc)
	}
}

func TestDecryptLegacyPlaintext(t *testing.T) {
	c, _ := New("rahasia-test-123")
	got, err := c.Decrypt("data-lama-tanpa-enkripsi")
	if err != nil {
		t.Fatalf("Decrypt data lama: %v", err)
	}
	if got != "data-lama-tanpa-enkripsi" {
		t.Fatalf("data lama berubah: got %q", got)
	}
}

func TestDecryptCorrupted(t *testing.T) {
	c, _ := New("rahasia-test-123")
	enc, _ := c.Encrypt("rahasia")
	bad := enc[:len(enc)-4] + "AAAA"
	if _, err := c.Decrypt(bad); err == nil {
		t.Fatal("harusnya gagal saat data rusak")
	}
}

func TestEmptyValue(t *testing.T) {
	c, _ := New("rahasia-test-123")
	enc, err := c.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("Encrypt kosong: %q, %v", enc, err)
	}
	got, err := c.Decrypt("")
	if err != nil || got != "" {
		t.Fatalf("Decrypt kosong: %q, %v", got, err)
	}
}

func TestNewRejectsEmptySecret(t *testing.T) {
	if _, err := New("  "); err == nil {
		t.Fatal("secret kosong harus ditolak")
	}
}