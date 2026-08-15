# Bot Admin WhatsApp

Bot layanan pelanggan + dashboard admin untuk toko/UMKM berbasis **WhatsApp**. Menangani katalog produk, pemesanan, broadcast, riwayat chat, dan menjawab pertanyaan pelanggan dengan **AI (LLM)** memakai data toko yang diunggah (PDF/CSV/XLSX) — semuanya dari satu aplikasi Go.

---

## Daftar Isi

1. [Fitur](#fitur)
2. [Arsitektur](#arsitektur)
3. [Kebutuhan](#kebutuhan)
4. [Instalasi & Menjalankan](#instalasi--menjalankan)
5. [Konfigurasi (.env)](#konfigurasi-env)
6. [Menghubungkan WhatsApp (gowa)](#menghubungkan-whatsapp-gowa)
7. [Dashboard Admin](#dashboard-admin)
8. [Perintah Admin via WhatsApp](#perintah-admin-via-whatsapp)
9. [Alur Pesanan Pelanggan](#alur-pesanan-pelanggan)
10. [AI & Knowledge Base (RAG)](#ai--knowledge-base-rag)
11. [Broadcast Massal](#broadcast-massal)
12. [Struktur Database](#struktur-database)
13. [Endpoint HTTP](#endpoint-http)
14. [Keamanan](#keamanan)
15. [Pemecahan Masalah](#pemecahan-masalah)
16. [Lisensi & Kredit](#lisensi--kredit)

---

## Fitur

| Fitur | Keterangan |
|---|---|
| 🤖 **Bot order WhatsApp** | Katalog → pilih produk → jumlah → alamat → konfirmasi → order masuk DB |
| 🧠 **AI menjawab pelanggan** | DeepSeek, Groq, NVIDIA NIM, Gemini, atau API OpenAI-compatible |
| 📄 **Knowledge base (RAG)** | Upload PDF/CSV/XLSX/TXT berisi katalog & harga; AI menjawab dari data itu |
| 💬 **Memori percakapan** | Bot ingat riwayat chat customer untuk jawaban kontekstual |
| 📢 **Broadcast massal** | Kirim ke semua pelanggan aktif dengan jeda anti-ban |
| 👥 **Manajemen pelanggan** | Blokir/buka blokir, lihat riwayat chat, catatan |
| 📦 **Manajemen produk** | CRUD produk, stok, harga, aktif/nonaktif |
| 🛒 **Manajemen pesanan** | Ubah status → notifikasi WhatsApp otomatis ke customer |
| ⚡ **Balasan cepat** | Kata kunci → balasan otomatis |
| 📱 **Akun WhatsApp** | Kelola akun gowa, login QR, set webhook |
| 🎛️ **Dashboard admin web** | Tampilan ala gowa (Tailwind/shadcn), mobile-friendly |

---

## Arsitektur

```
                        ┌─────────────────────────────────────────────┐
                        │              bot-admin-whatsapp              │
                        │                                             │
 WhatsApp pelanggan     │  ┌──────────┐   ┌──────────┐   ┌─────────┐  │
 ───────────────────►   │  │  gowa    │──►│  Fiber   │   │ router  │  │
  (chat ke nomor bot)   │  │  API     │   │  HTTP    │   │ (bot)   │  │
                        │  │ :3000    │   │  :8080   │   └────┬────┘  │
                        │  └──────────┘   └────┬─────┘        │       │
                        │                      │              ▼       │
                        │              /webhook/gowa     ┌─────────┐  │
                        │              (async)           │  store  │  │
                        │                                │ (pgx)   │  │
                        │              ┌──────────┐      └────┬────┘  │
                        │              │   ai     │───────────►│       │
                        │              │  (LLM)   │            ▼       │
                        │              └──────────┘      ┌─────────┐  │
                        │              broadcast worker  │ Postgres│  │
                        │              (goroutine)       │  5433   │  │
                        │                                └─────────┘  │
                        └─────────────────────────────────────────────┘
```

**Komponen kode (paket Go):**

| Paket | Peran |
|---|---|
| `main.go` | Entrypoint: server Fiber, webhook, broadcast worker, shutdown |
| `internal/config` | Baca `.env` (godotenv) |
| `internal/store` | Akses Postgres (pgxpool) + migrasi SQL |
| `internal/gowaclient` | Klien API **gowa** (go-whatsapp-web-multidevice): kirim pesan, device, QR, webhook |
| `internal/router` | Logika bot: webhook, alur order, perintah admin, quick replies, AI fallback |
| `internal/ai` | Klien LLM multi-provider + RAG (knowledge base) + memori percakapan |
| `internal/dashboard` | Dashboard admin web (template HTML + Tailwind) |

---

## Kebutuhan

- **Go** 1.26+ ([download](https://go.dev/dl/))
- **PostgreSQL** 14+ (jalankan lokal/docker)
- **gowa** — [go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice) berjalan di `:3000` (pakai nomor bot)
- (Opsional) API key salah satu penyedia AI

---

## Instalasi & Menjalankan

### Mode Postgres (default)

```bash
# 1. Clone & masuk
git clone <repo-url> && cd bot-admin-whatsapp

# 2. Buat database
createdb -h 127.0.0.1 -p 5433 bot_admin_whatsapp   # sesuaikan kredensial

# 3. Konfigurasi
cp .env.example .env
nano .env    # isi DATABASE_URL, DASHBOARD_PASSWORD, dll.

# 4. Build & jalankan (migrasi otomatis saat start)
go build -o bot-admin-whatsapp .
./bot-admin-whatsapp
# atau langsung:
go run .
```

### Mode SQLite (tanpa server, cocok untuk Windows .exe)

```bash
# Konfigurasi: ubah DB_DRIVER=sqlite (file DB otomatis dibuat, migrasi otomatis)
DB_DRIVER=sqlite SQLITE_PATH=bot_admin_whatsapp.db ./bot-admin-whatsapp
```

Database tersimpan sebagai satu file lokal (`SQLITE_PATH`), tidak perlu instal Postgres. `go run .` tetap jalan untuk development.

### Build Windows .exe

```bash
# Di mesin mana pun (Linux/macOS/Windows) dengan Go terpasang:
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bot-admin-whatsapp.exe .
```

Hasilnya satu file `bot-admin-whatsapp.exe` (murni Go, tanpa CGO). Tinggal buat `.env` di sebelahnya dengan `DB_DRIVER=sqlite`, lalu jalankan — semua (server, dashboard, webhook, SQLite) berjalan dalam satu proses.

Saat pertama start:
- Migrasi SQL dijalankan otomatis (`internal/store/migrations/postgres/*.sql` atau `internal/store/migrations/sqlite/*.sql` sesuai dialect)
- Server mendengar di `:8080`
- Dashboard di `http://localhost:8080/admin`
- Webhook gowa di `POST http://localhost:8080/webhook/gowa`

> Pastikan gowa sudah berjalan di `:3000` sebelum login QR.

---

## Konfigurasi (.env)

Lihat [.env.example](.env.example) untuk template lengkap.

| Variabel | Default | Keterangan |
|---|---|---|
| `PORT` | `8080` | Port server HTTP |
| `DB_DRIVER` | `postgres` | `postgres` (butuh server) atau `sqlite` (file lokal, tanpa server — untuk Windows .exe) |
| `DATABASE_URL` | `postgres://dirman@127.0.0.1:5433/...` | DSN Postgres (dipakai saat `DB_DRIVER=postgres`) |
| `SQLITE_PATH` | `bot_admin_whatsapp.db` | Path file DB SQLite (dipakai saat `DB_DRIVER=sqlite`) |
| `GOWA_BASE_URL` | `http://127.0.0.1:3000` | URL API gowa |
| `GOWA_WEBHOOK_URL` | `http://127.0.0.1:8080/webhook/gowa` | URL yang didaftarkan ke gowa |
| `GOWA_WEBHOOK_SECRET` | `secret` | Secret HMAC webhook (`X-Hub-Signature-256`) — harus sama dengan secret webhook device di gowa |
| `ADMIN_PHONE` | `628123456789` | Nomor admin (format internasional tanpa `+`) |
| `DASHBOARD_USER` | `admin` | Login dashboard |
| `DASHBOARD_PASSWORD` | `admin123` | **Ganti!** |
| `SESSION_SECRET` | — | Kunci sesi (HMAC) |
| `BROADCAST_DELAY_SECONDS` | `7` | Jeda antar pesan broadcast (anti-ban) |
| `STORE_NAME` | `Toko Kita` | Nama toko yang tampil di pesan bot |
| `AI_PROVIDER` | — | `deepseek` \| `groq` \| `nim` \| `gemini` \| `custom` |
| `AI_API_KEY` | — | API key LLM |
| `AI_BASE_URL` | — | Base URL (boleh dikosongkan, preset otomatis) |
| `AI_MODEL` | — | Nama model |

> Nilai AI juga bisa diatur dari **Dashboard → AI & Data** (tersimpan di DB dan menang atas `.env`).

---

## Menghubungkan WhatsApp (gowa)

1. Jalankan gowa (`:3000`) dan pastikan `GOWA_BASE_URL` sesuai.
2. Buka dashboard → **Akun WA** → isi username/password akun gowa → **Simpan Akun** (kredensial diverifikasi otomatis).
3. Klik **QR Login** pada akun aktif → scan QR dengan WhatsApp di HP (**Setelan → Perangkat Tertaut → Tautkan perangkat**).
4. Klik **Set Webhook** → gowa akan meneruskan setiap pesan ke bot.
5. Selesai. Tes dengan chat *"menu"* ke nomor bot.

---

## Dashboard Admin

Akses `http://localhost:8080/admin` (login `DASHBOARD_USER`/`DASHBOARD_PASSWORD`).

| Menu | Fungsi |
|---|---|
| **Ringkasan** | Statistik hari ini + pesanan terbaru |
| **Pesanan** | Filter status, ubah status → notifikasi WA ke customer |
| **Produk** | Tambah/ubah/hapus produk (harga, stok, aktif) |
| **Pelanggan** | Cari, blokir/buka, lihat riwayat chat |
| **Broadcast** | Buat broadcast (dikirim otomatis oleh worker) |
| **Balasan Cepat** | Kata kunci → balasan otomatis |
| **Akun WA** | Kelola akun gowa, QR login, webhook |
| **AI & Data** | Konfigurasi LLM + upload knowledge base |

---

## Perintah Admin via WhatsApp

Dari nomor `ADMIN_PHONE`, kirim perintah berawalan `/`:

| Perintah | Fungsi |
|---|---|
| `/ringkasan` atau `/stats` | Ringkasan order hari ini |
| `/menu` | Kirim katalog ke nomor admin |
| `/produk` | Daftar produk & stok |
| `/blokir <nomor>` | Blokir customer |
| `/buka <nomor>` | Buka blokir customer |
| `/bantuan` atau `/help` | Daftar perintah |

---

## Alur Pesanan Pelanggan

```
Pelanggan → "menu" / "start" / "katalog"
   └─► Bot kirim katalog (produk aktif + harga + stok)
Pelanggan → nomor produk (mis. "1")
   └─► Bot minta jumlah
Pelanggan → "2"
   └─► Bot minta alamat
Pelanggan → alamat lengkap
   └─► Bot tampilkan ringkasan + minta konfirmasi ("ya")
Pelanggan → "ya"
   └─► Order dibuat (INV-YYYYMMDD-0001)
       ├─► Notifikasi ORDER BARU ke admin
       └─► Konfirmasi ke pelanggan
```

- Ketik **"batal"** kapan saja untuk membatalkan pesanan.
- Stok dicek saat pilih produk & jumlah; stok `-1` = tidak terbatas.
- Status order: `baru → diproses → dikirim → selesai` (atau `batal`).
- Setiap perubahan status dari dashboard mengirim notifikasi ke customer.

---

## AI & Knowledge Base (RAG)

### Penyedia yang didukung

Semua memakai protokol OpenAI-compatible (`/chat/completions`):

| Penyedia | Base URL | Model default |
|---|---|---|
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-chat` |
| Groq | `https://api.groq.com/openai/v1` | `llama-3.3-70b-versatile` |
| NVIDIA NIM | `https://integrate.api.nvidia.com/v1` | `meta/llama-3.3-70b-instruct` |
| Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` | `gemini-2.0-flash` |
| Custom | terserah | terserah |

### Cara kerja

1. **Unggah data** ke Dashboard → **AI & Data** (PDF/CSV/XLSX/TXT, maks 25 MB).
2. Teks diekstrak; baris tabel diubah menjadi **self-describing** (`nama: X | harga: Y | ...`) agar pencarian akurat.
3. Teks di-**chunk** (~600 karakter) dan diindeks di Postgres (full-text `tsvector` + cadangan `ILIKE`).
4. Saat pelanggan bertanya, bot:
   - mengambil **data relevan** (ranking jumlah kata yang cocok),
   - menyertakan **riwayat percakapan** (memori, max 8 pesan),
   - mengirim prompt ke LLM dengan instruksi *"jawab berdasarkan data toko; jangan mengarang"*,
   - menjawab pelanggan. Jika AI mati → balasan default.

> Alur pesanan (`menu`, nomor produk, alamat, `ya`) **tetap prioritas** — AI hanya menjawab pertanyaan yang tidak cocok dengan alur/balasan cepat.

---

## Broadcast Massal

- Buat di **Dashboard → Broadcast** (pesan + segmen).
- Worker memeriksa antrean setiap 15 detik; broadcast `pending` dikirim ke semua pelanggan aktif (kecuali diblokir/admin).
- Jeda antar pesan mengikuti `BROADCAST_DELAY_SECONDS` (default 7 detik) untuk menghindari pemblokiran WhatsApp.
- Progres `sent`/`failed` diperbarui real-time; status: `pending → running → done/failed`.

---

## Struktur Database

Migrasi: `internal/store/migrations/`

| Tabel | Isi |
|---|---|
| `customers` | Pelanggan (phone unik, jid, nama, catatan, status `active`/`blocked`) |
| `products` | Produk (harga BIGINT rupiah, stok, `is_active`, path gambar) |
| `orders` | Order (nomor unik `INV-...`, status, total, alamat) |
| `order_items` | Item per order (nama, harga, qty) |
| `chat_messages` | Riwayat percakapan (arah in/out, tipe, body) |
| `quick_replies` | Balasan cepat (keyword unik) |
| `broadcasts` | Broadcast + progres |
| `order_sessions` | State alur order per customer |
| `wa_accounts` | Akun gowa (token tersimpan, device id, aktif) |
| `settings` | Key-value (termasuk konfigurasi AI) |
| `knowledge_docs` | Dokumen knowledge base (bytea) |
| `knowledge_chunks` | Potongan teks terindeks (GIN tsvector) |
| `schema_migrations` | Versi migrasi yang sudah dijalankan |

---

## Endpoint HTTP

### Publik
| Method | Path | Keterangan |
|---|---|---|
| GET | `/health` | Health check |
| POST | `/webhook/gowa` | Webhook pesan dari gowa (balas 200 langsung, proses async) |

### Dashboard (perlu sesi login)
Semua di bawah `/admin` — lihat [docs/api.md](docs/api.md) untuk detail lengkap.

| Method | Path | Fungsi |
|---|---|---|
| GET/POST | `/admin/login`, POST `/admin/logout` | Autentikasi |
| GET | `/admin/overview`, `/admin/orders`, `/admin/products`, `/admin/customers`, `/admin/broadcast`, `/admin/replies`, `/admin/accounts`, `/admin/ai` | Halaman |
| POST | `/admin/orders/:id/status`, `/admin/products`, `/admin/products/:id`, `/admin/products/:id/delete` | Kelola order & produk |
| POST | `/admin/customers/:id/status`, `/admin/customers/:id/chat` | Kelola pelanggan |
| POST | `/admin/broadcast`, `/admin/replies`, `/admin/replies/:id/toggle`, `/admin/replies/:id/delete` | Broadcast & balasan |
| POST | `/admin/accounts`, `/admin/accounts/:id/active`, `/admin/accounts/:id/webhook`, GET `/admin/accounts/:id/qr` | Akun gowa |
| POST | `/admin/ai`, `/admin/ai/test`, `/admin/ai/knowledge` (multipart), `/admin/ai/knowledge/:id/delete` | AI & knowledge base |
| GET | `/admin/static/*` | Aset statis (CSS) |

---

## Keamanan

- **Jangan commit `.env`** (sudah di-`.gitignore`).
- Ganti `DASHBOARD_PASSWORD` dan `SESSION_SECRET` sebelum dipakai produksi.
- Webhook memverifikasi header `X-Webhook-Secret` bila `GOWA_WEBHOOK_SECRET` diubah dari default.
- Sesi dashboard berupa cookie HttpOnly + HMAC (`SESSION_SECRET`).
- Password akun gowa & token tersimpan di DB (`wa_accounts`) — lindungi akses DB.
- API key AI tersimpan di tabel `settings` — jangan tampilkan ke publik.
- Saran produksi: jalankan di belakang reverse proxy dengan HTTPS (mis. Caddy/Nginx).

---

## Pemecahan Masalah

| Gejala | Solusi |
|---|---|
| `connect postgres` gagal | Pastikan Postgres jalan & `DATABASE_URL` benar (atau ganti `DB_DRIVER=sqlite`) |
| Belum ada akun gowa aktif | Dashboard → Akun WA → tambah akun → aktifkan |
| QR tidak muncul | Akun aktif & device dibuat; muat ulang halaman |
| Pesan pelanggan tidak masuk | Cek **Set Webhook** pada akun aktif; pastikan gowa login |
| AI tidak menjawab | Dashboard → AI & Data: isi API key, **Uji Koneksi**, centang aktif |
| AI menjawab tapi tidak sesuai data | Upload knowledge base yang benar; pastikan kata kunci ada di file |
| Broadcast tidak terkirim | Pastikan ada pelanggan aktif; cek log (`broadcast N selesai`) |
| Broadcast kena banned WhatsApp | Naikkan `BROADCAST_DELAY_SECONDS` |

Log berjalan di stdout/stderr (saat dijalankan manual) atau file (`nohup ... > app.log 2>&1 &`).

---

## Lisensi & Kredit

- Icon & gaya dashboard mengikuti **Tailwind CSS v4** dan ikon **Lucide** (MIT).
- Parser XLSX: [excelize](https://github.com/xuri/excelize) (BSD-3), PDF: [ledongthuc/pdf](https://github.com/ledongthuc/pdf) (MIT).
- Framework web: [Fiber](https://gofiber.io) (MIT), driver DB: [pgx](https://github.com/jackc/pgx) (MIT).
- Backend WhatsApp: [go-whatsapp-web-multidevice (gowa)](https://github.com/aldinokemal/go-whatsapp-web-multidevice).
