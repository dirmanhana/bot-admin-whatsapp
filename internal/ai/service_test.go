package ai

import (
	"strings"
	"testing"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func TestFormatPrice(t *testing.T) {
	cases := map[int64]string{
		0:      "Rp0",
		500:    "Rp500",
		25000:  "Rp25.000",
		1234567: "Rp1.234.567",
	}
	for in, want := range cases {
		if got := formatPrice(in); got != want {
			t.Errorf("formatPrice(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestQueryWords(t *testing.T) {
	words := queryWords("berapa harga produk X?")
	if len(words) == 0 {
		t.Fatal("queryWords returned empty")
	}
	for _, w := range words {
		if len(w) < 3 {
			t.Errorf("queryWords contains short word %q", w)
		}
	}
}

func TestProductContextRelevantOnly(t *testing.T) {
	products := []store.Product{
		{Name: "Kopi Arabika", Description: "bubuk kopi premium", Price: 50000, Stock: 10},
		{Name: "Teh Hijau", Description: "daun teh pilihan", Price: 30000, Stock: 0},
		{Name: "Susu UHT", Description: "susu sapi murni", Price: 20000, Stock: 5},
	}
	ctx := productContext(products, "berapa harga teh?", 15)
	if !strings.Contains(ctx, "Teh Hijau") {
		t.Errorf("context harus berisi produk relevan: %s", ctx)
	}
	if strings.Contains(ctx, "Kopi Arabika") || strings.Contains(ctx, "Susu UHT") {
		t.Errorf("context tidak boleh berisi produk tak relevan: %s", ctx)
	}
}

func TestProductContextFallback(t *testing.T) {
	products := []store.Product{
		{Name: "Kopi Arabika", Price: 50000, Stock: 10},
		{Name: "Teh Hijau", Price: 30000, Stock: 5},
	}
	ctx := productContext(products, "zzz kata tak dikenal", 15)
	if !strings.Contains(ctx, "Kopi Arabika") {
		t.Errorf("tanpa kecocokan harus ada produk fallback: %s", ctx)
	}
}

func TestProductContextEmpty(t *testing.T) {
	if ctx := productContext(nil, "halo", 15); ctx != "" {
		t.Errorf("productContext(nil) = %q, want empty", ctx)
	}
}

func TestProductContextMaxShown(t *testing.T) {
	products := []store.Product{
		{Name: "Produk Satu", Price: 10000}, {Name: "Produk Dua", Price: 20000},
		{Name: "Produk Tiga", Price: 30000}, {Name: "Produk Empat", Price: 40000},
	}
	ctx := productContext(products, "produk", 2)
	if strings.Count(ctx, "- Produk") != 2 {
		t.Errorf("maxShown=2 harus membatasi: %s", ctx)
	}
}

func TestSystemPromptIncludesStore(t *testing.T) {
	p := systemPrompt("Toko Kita", "Jl. Merdeka 1", "628123", "ramah", "Rara", "08.00-17.00", "QRIS, BCA", true)
	for _, want := range []string{"Rara", "Toko Kita", "Jl. Merdeka 1", "628123", "08.00-17.00", "QRIS, BCA", "ramah"} {
		if !strings.Contains(p, want) {
			t.Errorf("systemPrompt tidak memuat %q: %s", want, p)
		}
	}
}

func TestSystemPromptNoData(t *testing.T) {
	p := systemPrompt("Toko", "", "", "", "", "", "", false)
	if strings.Contains(p, "DATA TOKO") {
		t.Errorf("tanpa data tidak boleh menyebut DATA TOKO: %s", p)
	}
	if !strings.Contains(p, "Jangan mengarang informasi spesifik") {
		t.Errorf("tanpa data harus menegaskan jangan mengarang: %s", p)
	}
}