package router

import (
	"strconv"
	"strings"

	"github.com/dirman/bot-admin-whatsapp/internal/store"
)

// WATarget returns the address to use when replying to a customer: for
// LID-based accounts (WhatsApp hides the phone number behind a @lid JID) it
// returns the full JID, otherwise the plain phone number.
func WATarget(c *store.Customer) string {
	if c == nil {
		return ""
	}
	if c.JID != "" && strings.HasSuffix(c.JID, "@lid") {
		return c.JID
	}
	return c.Phone
}

// ExtractPhone returns the phone number part of a WhatsApp JID, dropping any
// country-code suffix domain (e.g. "628123456789@s.whatsapp.net" -> "628123456789").
func ExtractPhone(jid string) string {
	jid = strings.TrimSpace(jid)
	if i := strings.Index(jid, "@"); i >= 0 {
		jid = jid[:i]
	}
	return jid
}

// Normalize lowercases and trims a message body for keyword matching.
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ParseNumber extracts the leading integer from a normalized string
// ("2", "2x", "3 pcs", "2x3"). Returns 0 when none found.
func ParseNumber(s string) int {
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) > 0 {
		first := strings.SplitN(fields[0], "x", 2)[0]
		if n, err := strconv.Atoi(strings.TrimSpace(first)); err == nil {
			return n
		}
	}
	return 0
}

// FormatPrice renders an amount in Rupiah with thousands separators.
func FormatPrice(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp" + b.String()
	if neg {
		out = "-" + out
	}
	return out
}
