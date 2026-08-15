package dashboard

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web/style.css web/templates/*.html
var webFS embed.FS

// lucideIcons adalah koleksi icon lucide (MIT License) — icon yang sama
// dengan yang dipakai dashboard gowa. Value = inner SVG path.
var lucideIcons = map[string]string{
	"dashboard":  `<rect width="7" height="9" x="3" y="3" rx="1"/><rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/><rect width="7" height="5" x="3" y="16" rx="1"/>`,
	"cart":       `<circle cx="8" cy="21" r="1"/><circle cx="19" cy="21" r="1"/><path d="M2.05 2.05h2l2.66 12.42a2 2 0 0 0 2 1.58h9.78a2 2 0 0 0 1.95-1.57l1.65-7.43H5.12"/>`,
	"package":    `<path d="m7.5 4.27 9 5.15"/><path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/>`,
	"users":      `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>`,
	"megaphone":  `<path d="m3 11 18-5v12L3 14v-3z"/><path d="M11.6 16.8a3 3 0 1 1-5.8-1.6"/>`,
	"zap":        `<path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z"/>`,
	"phone":      `<rect width="14" height="20" x="5" y="2" rx="2" ry="2"/><path d="M12 18h.01"/>`,
	"search":     `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
	"arrow-left": `<path d="m12 19-7-7 7-7"/><path d="M19 12H5"/>`,
	"logout":     `<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/>`,
	"plus":       `<path d="M5 12h14"/><path d="M12 5v14"/>`,
	"trash":      `<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>`,
	"qr":         `<rect width="5" height="5" x="3" y="3" rx="1"/><rect width="5" height="5" x="16" y="3" rx="1"/><rect width="5" height="5" x="3" y="16" rx="1"/><path d="M21 16h-3a2 2 0 0 0-2 2v3"/><path d="M21 21v.01"/><path d="M12 7v3a2 2 0 0 1-2 2H7"/><path d="M3 12h.01"/><path d="M12 3h.01"/><path d="M12 16v.01"/><path d="M16 12h1"/><path d="M21 12v.01"/><path d="M12 21v-1"/>`,
	"webhook":    `<path d="M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 0 1 2 17c.01-.7.2-1.4.57-2"/><path d="m6 17 3.13-5.78c.53-.97.1-2.18-.5-3.1a4 4 0 1 1 6.89-4.06"/><path d="m12 6 3.13 5.73C15.66 12.7 16.9 13 18 13a4 4 0 0 1 0 8"/>`,
	"send":       `<path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/>`,
	"check":      `<path d="M20 6 9 17l-5-5"/>`,
	"x":          `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
	"refresh":    `<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/>`,
}

// icon renders a lucide SVG icon (16px) with currentColor stroke.
func icon(name string) template.HTML {
	p, ok := lucideIcons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="shrink-0">` + p + `</svg>`)
}

type templates struct {
	pages map[string]*template.Template
}

func loadTemplates() *templates {
	funcs := template.FuncMap{
		"price": func(v int64) string {
			neg := v < 0
			if neg {
				v = -v
			}
			s := itoa(v)
			var b strings.Builder
			for i := 0; i < len(s); i++ {
				if i > 0 && (len(s)-i)%3 == 0 {
					b.WriteByte('.')
				}
				b.WriteByte(s[i])
			}
			out := "Rp" + b.String()
			if neg {
				out = "-" + out
			}
			return out
		},
		"ftime": func(t time.Time) string { return t.Local().Format("02 Jan 15:04") },
		"statusLabel": func(s string) string {
			labels := map[string]string{
				"baru": "Baru", "diproses": "Diproses", "dikirim": "Dikirim",
				"selesai": "Selesai", "batal": "Batal", "pending": "Pending",
				"running": "Berjalan", "done": "Selesai", "failed": "Gagal",
			}
			if l, ok := labels[s]; ok {
				return l
			}
			return s
		},
		"badge": badgeClass,
		"icon":  icon,
	}
	baseHTML, err := webFS.ReadFile("web/templates/base.html")
	if err != nil {
		panic(err)
	}

	pages := map[string]*template.Template{}
	files, _ := fs.Glob(webFS, "web/templates/*.html")
	for _, f := range files {
		base := filepath.Base(f)
		if base == "base.html" {
			continue
		}
		name := strings.TrimSuffix(base, ".html")
		b, err := webFS.ReadFile(f)
		if err != nil {
			panic(err)
		}
		// Each page gets its own template set: base layout + page body,
		// so the shared "content" name never clashes across pages.
		t := template.New(name).Funcs(funcs)
		template.Must(t.Parse(string(baseHTML)))
		template.Must(t.Parse(string(b)))
		pages[name] = t
	}
	return &templates{pages: pages}
}

func (t *templates) ExecuteTemplate(w interface{ Write([]byte) (int, error) }, name string, data any) error {
	tpl, ok := t.pages[name]
	if !ok {
		return fmt.Errorf("template %q tidak ditemukan", name)
	}
	// Execute the page root directly: its body ends with
	// {{template "base" .}} which renders the full layout.
	return tpl.Execute(w, data)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}

func badgeClass(s string) string {
	switch s {
	case "baru", "pending", "diproses", "running", "active":
		return "badge badge-blue"
	case "dikirim", "selesai", "done":
		return "badge badge-green"
	case "batal", "failed", "blocked":
		return "badge badge-red"
	default:
		return "badge badge-gray"
	}
}
