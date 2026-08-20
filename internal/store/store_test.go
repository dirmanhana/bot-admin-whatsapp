package store

import "testing"

func TestToSQLitePlaceholders(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT * FROM t WHERE id = $1", "SELECT * FROM t WHERE id = ?"},
		{"VALUES ($1, $2, $3)", "VALUES (?, ?, ?)"},
		{"$1 = $2 AND $3", "? = ? AND ?"},
		{"no placeholders here", "no placeholders here"},
		{"nilai $10 setelah $1", "nilai ? setelah ?"},
	}
	for _, tc := range cases {
		if got := toSQLitePlaceholders(tc.in); got != tc.want {
			t.Errorf("toSQLitePlaceholders(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToSQLitePlaceholdersIgnoresPostgresCast(t *testing.T) {
	// $1::text harus menjadi ?::text (konversi literal tidak berubah).
	in := "WHERE id = $1::text"
	if got := toSQLitePlaceholders(in); got != "WHERE id = ?::text" {
		t.Errorf("toSQLitePlaceholders(%q) = %q", in, got)
	}
}