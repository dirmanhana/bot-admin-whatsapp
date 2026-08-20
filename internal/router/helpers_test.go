package router

import (
	"testing"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

func TestWATarget(t *testing.T) {
	cases := []struct {
		name string
		c    *store.Customer
		want string
	}{
		{"nil customer", nil, ""},
		{"plain phone", &store.Customer{Phone: "628123456789"}, "628123456789"},
		{"lid jid", &store.Customer{Phone: "628123456789", JID: "123456789@lid"}, "123456789@lid"},
		{"s.whatsapp jid", &store.Customer{Phone: "628123456789", JID: "628123456789@s.whatsapp.net"}, "628123456789"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WATarget(tc.c); got != tc.want {
				t.Errorf("WATarget() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractPhone(t *testing.T) {
	cases := map[string]string{
		"628123456789@s.whatsapp.net": "628123456789",
		"628123456789":                "628123456789",
		"  +6281 23 ":                 "+6281 23",
	}
	for in, want := range cases {
		if got := ExtractPhone(in); got != want {
			t.Errorf("ExtractPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("  MENU  "); got != "menu" {
		t.Errorf("Normalize() = %q, want %q", got, "menu")
	}
}

func TestParseNumber(t *testing.T) {
	cases := map[string]int{
		"2":     2,
		"2x":    2,
		"3 pcs": 3,
		"2x3":   2,
		"abc":   0,
		"":      0,
	}
	for in, want := range cases {
		if got := ParseNumber(in); got != want {
			t.Errorf("ParseNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFormatPrice(t *testing.T) {
	cases := map[int64]string{
		0:        "Rp0",
		1000:     "Rp1.000",
		1500000:  "Rp1.500.000",
		-25000:   "-Rp25.000",
		99999999: "Rp99.999.999",
	}
	for in, want := range cases {
		if got := FormatPrice(in); got != want {
			t.Errorf("FormatPrice(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAddToCart(t *testing.T) {
	items := addToCart(nil, 1, "A", 1000, 2)
	if len(items) != 1 || items[0].Qty != 2 {
		t.Fatalf("addToCart first = %+v, want 1 item qty 2", items)
	}
	items = addToCart(items, 1, "A", 1000, 3)
	if len(items) != 1 || items[0].Qty != 5 {
		t.Errorf("addToCart same product: %+v, want qty 5", items)
	}
	items = addToCart(items, 2, "B", 500, 1)
	if len(items) != 2 {
		t.Errorf("addToCart second product: %+v, want 2 items", items)
	}
}
func TestExtractQty(t *testing.T) {
	cases := map[string]int{
		"saya mau pesan bakso kering 10 qty": 10,
		"saya mau pesan 10 pcs":              10,
		"2 pcs":                              2,
		"5x":                                 5,
		"3 buah":                             3,
		"pesan 1":                            1,
		"tidak ada angka":                    0,
		"":                                   0,
	}
	for in, want := range cases {
		if got := ExtractQty(in); got != want {
			t.Errorf("ExtractQty(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestContainsOrderIntent(t *testing.T) {
	cases := map[string]bool{
		"saya mau pesan bakso": true,
		"mau beli 2":           true,
		"order 1 pcs":          true,
		"berapa harga bakso?":  false,
		"halo apa kabar":       false,
	}
	for in, want := range cases {
		if got := containsOrderIntent(in); got != want {
			t.Errorf("containsOrderIntent(%q) = %v, want %v", in, got, want)
		}
	}
}
