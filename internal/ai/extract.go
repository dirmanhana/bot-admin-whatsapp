package ai

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// AllowedExts adalah ekstensi file yang bisa diunggah sebagai pengetahuan.
var AllowedExts = map[string]bool{
	".pdf": true, ".csv": true, ".xlsx": true, ".txt": true, ".md": true,
}

// ExtractText mengubah isi file (PDF/CSV/XLSX/TXT) menjadi teks polos.
func ExtractText(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return extractPDF(data)
	case ".csv":
		return extractCSV(data)
	case ".xlsx":
		return extractXLSX(data)
	case ".txt", ".md":
		return string(data), nil
	default:
		return "", fmt.Errorf("format tidak didukung: %s (dukung: pdf, csv, xlsx, txt)", ext)
	}
}

func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("baca PDF: %w", err)
	}
	tr, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("ekstrak PDF: %w", err)
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func extractCSV(data []byte) (string, error) {
	rd := csv.NewReader(bytes.NewReader(data))
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse CSV: %w", err)
	}
	return rowsToText(rows), nil
}

func extractXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("buka XLSX: %w", err)
	}
	defer f.Close()

	var b strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(rowsToText(rows))
	}
	return strings.TrimSpace(b.String()), nil
}

// rowsToText mengubah baris tabel menjadi teks self-describing.
// Baris pertama yang terlihat seperti header dipakai sebagai label kolom,
// sehingga setiap baris menjadi: "nama: X | harga: Y | ..." — ini membuat
// pencarian teks jauh lebih akurat (mis. pertanyaan "harga kopi" cocok
// dengan baris produk, bukan hanya header).
func rowsToText(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	labels := []string{}
	start := 0
	if looksLikeHeader(rows[0]) && len(rows) > 1 {
		labels = rows[0]
		start = 1
	}
	var b strings.Builder
	for i := start; i < len(rows); i++ {
		row := rows[i]
		if i > start {
			b.WriteString("\n")
		}
		parts := make([]string, 0, len(row))
		for j, cell := range row {
			cell = strings.TrimSpace(cell)
			if cell == "" {
				continue
			}
			if j < len(labels) && labels[j] != "" {
				parts = append(parts, strings.TrimSpace(labels[j])+": "+cell)
			} else {
				parts = append(parts, cell)
			}
		}
		b.WriteString(strings.Join(parts, " | "))
	}
	return strings.TrimSpace(b.String())
}

// looksLikeHeader menebak apakah baris pertama adalah header (sebagian besar
// sel bukan angka).
func looksLikeHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	nonNumeric := 0
	for _, c := range row {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, err := strconv.ParseFloat(strings.ReplaceAll(c, ",", ""), 64); err != nil {
			nonNumeric++
		}
	}
	return nonNumeric >= (len(row)+1)/2
}

// ChunkText memecah teks menjadi potongan-potongan kecil (~600 karakter)
// agar retrieval dan konteks LLM lebih efektif.
func ChunkText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	const maxChunk = 600

	lines := strings.Split(text, "\n")
	var chunks []string
	var cur strings.Builder

	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}

	for _, ln := range lines {
		// Baris sangat panjang (mis. satu sel berisi paragraf): pecah per kata.
		if len(ln) > maxChunk {
			if cur.Len() > 0 {
				flush()
			}
			for _, part := range splitLong(ln, maxChunk) {
				chunks = append(chunks, part)
			}
			continue
		}
		if cur.Len() > 0 && cur.Len()+len(ln)+1 > maxChunk {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(ln)
	}
	flush()
	return chunks
}

func splitLong(s string, n int) []string {
	words := strings.Fields(s)
	var out []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+len(w)+1 > n {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(w)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}
