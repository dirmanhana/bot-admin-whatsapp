# Arsitektur & Desain

## Gambaran Umum

Aplikasi Go monolitik dengan tiga peran utama:

1. **Server HTTP** (Fiber) — dashboard admin + endpoint webhook gowa.
2. **Bot WhatsApp** — logika percakapan yang berjalan async dari webhook.
3. **Worker background** — pengirim broadcast.

Semua state di Postgres; gowa sebagai jembatan ke WhatsApp.

---

## Diagram Alur Pesan

```
 gowa (WhatsApp)
     │  POST /webhook/gowa (X-Webhook-Secret)
     ▼
 HandleWebhook (router)                    ← balas 200 seketika
     │
     ├─ filter: is_from_me / grup / status / broadcast → stop
     ├─ ExtractPhone → GetOrCreateCustomer
     ├─ SaveChatMessage (arah "in")
     ├─ blocked? → stop
     ├─ admin + "/" → handleAdminCommand
     └─ handleCustomerMessage
            ├─ "menu/start/katalog/mulai" → sendCatalog + sesi selecting_product
            ├─ "batal/cancel" → hapus sesi
            ├─ sesi order aktif → handleOrderSession
            │      selecting_product → entering_qty → entering_address → confirming
            │      "ya" → createOrder → notifikasi admin + konfirmasi
            ├─ quick reply cocok → balas
            ├─ AI aktif → Answer(question, history) → balas
            └─ default reply
```

## Diagram Alur AI (RAG)

```
Pertanyaan pelanggan + riwayat (memori)
        │
        ▼
SearchKnowledgeChunks(query = pertanyaan + pesan terakhir, 5)
        │  1) tsvector @@ plainto_tsquery('simple', q)
        │  2) fallback: ILIKE per kata (OR)
        │  3) ranking Go: hitung kata pertanyaan yang muncul di chunk
        ▼
chunks teratas
        │
        ▼
Prompt = [RIWAYAT PERCAKAPAN] + PERTANYAAN + DATA TOKO
        │
        ▼
POST {base_url}/chat/completions  (Bearer <api_key>, OpenAI-compatible)
        │
        ▼
Jawaban → kirim ke pelanggan → SaveChatMessage (arah "out")
```

## Diagram Broadcast

```
Dashboard: POST /admin/broadcast → baris status=pending
        │
        ▼
Worker (tiap 15 detik) → runPendingBroadcasts
        │
        ├─ SetBroadcastRunning (pending→running)
        ├─ ambil pelanggan aktif (bukan admin/blocked)
        ├─ loop: SendText → progress sent/failed
        │        jeda BROADCAST_DELAY_SECONDS per pesan
        └─ FinishBroadcast (done/failed)
```

---

## Struktur Paket

```
main.go                     — wiring: config → store → gowa → ai → router → dashboard
internal/
├── config/config.go        — Config struct + Load() dari .env
├── store/
│   ├── store.go            — semua query/transaksi
│   ├── models.go           — struct domain (Customer, Order, Product, ...)
│   ├── migrate.go          — migrator (embed FS, tabel schema_migrations)
│   └── migrations/
│       ├── 0001_init.sql   — skema inti
│       └── 0002_ai.sql     — knowledge base
├── gowaclient/client.go    — Login, EnsureToken, SendText, SendImage,
│                             DeviceStatus, LoginDeviceQR, SetDeviceWebhook
├── router/
│   ├── router.go           — HandleWebhook, alur customer & order
│   ├── admin.go            — perintah admin WhatsApp (/ringkasan, /blokir, ...)
│   └── helpers.go          — ExtractPhone, Normalize, ParseNumber, FormatPrice
├── ai/
│   ├── client.go           — provider LLM + Chat() OpenAI-compatible
│   ├── service.go          — Settings (cache 60s), SaveSettings, Answer (RAG+memori)
│   └── extract.go          — ekstraksi PDF/CSV/XLSX/TXT + chunking
└── dashboard/
    ├── dashboard.go        — Server, routes, auth (cookie HMAC), static
    ├── handlers.go         — halaman GET
    ├── actions.go          — aksi POST
    ├── ai.go               — halaman/aksi AI & knowledge base
    ├── templates.go        — engine html/template (per-halaman), funcs, icon lucide
    └── web/
        ├── style.css       — Tailwind v4 + token warna gowa (329KB)
        └── templates/*.html
```

---

## Keputusan Desain

### 1. Webhook async
`HandleWebhook` dipanggil dalam goroutine agar gowa menerima 200 seketika; pekerjaan berat (DB, AI) tidak memblokir HTTP.

### 2. Multi-provider AI via satu protokol
DeepSeek, Groq, NIM, dan Gemini semuanya menyediakan endpoint OpenAI-compatible `chat/completions`, sehingga satu `Client` cukup — cukup ubah `base_url`, `model`, `api_key`.

### 3. RAG tanpa embedding API
Agar hemat & minim dependensi eksternal, retrieval memakai:
- Postgres **full-text search** (`to_tsvector('simple', ...)` + GIN index),
- fallback **ILIKE**,
- **ranking kata** di Go (jumlah kata pertanyaan yang muncul di chunk, tie-break panjang chunk).

Data tabular (CSV/XLSX) diubah menjadi **self-describing** (`nama: X | harga: Y`) sehingga pertanyaan "harga kopi" cocok dengan baris produk, bukan hanya header.

### 4. Memori percakapan
Pertanyaan kontekstual ("yang tadi berapa?") tidak punya kata kunci produk. Solusi:
- Prompt menyertakan riwayat (max 8 pesan, masing-masing ≤300 karakter),
- query retrieval = pertanyaan **+ pesan terakhir sebelum pertanyaan** (baik dari customer maupun balasan bot).

### 5. Sesi order tanpa stateful server
Alur order disimpan di tabel `order_sessions` (state machine per customer) — server boleh restart tanpa kehilangan proses.

### 6. Broadcast aman
Jeda wajib (`BROADCAST_DELAY_SECONDS`) + status transisi `pending → running → done/failed` agar tidak dobel kirim saat worker restart.

### 7. Template per-halaman
`html/template` dibuat **satu set per halaman** (base + body) untuk menghindari bentrok `define "content"` pada namespace bersama.

---

## Skema Database (ringkas)

```
customers(id, phone UNIQUE, jid, name, notes, status, created_at, updated_at)
products(id, name, description, price, image_path, stock, is_active, ...)
orders(id, order_number UNIQUE, customer_id→customers, status, total, address, note, ...)
order_items(id, order_id→orders CASCADE, product_id→products, product_name, price, qty)
chat_messages(id, customer_id→customers, direction, message_type, body, media_url, wa_message_id, created_at)
  INDEX (customer_id, created_at DESC)
quick_replies(id, keyword UNIQUE, reply, is_active, created_at)
broadcasts(id, message, segment, total_targets, sent, failed, status, created_at, finished_at)
order_sessions(customer_id PK→customers, state, product_id, qty, address, updated_at)
wa_accounts(id, username UNIQUE, password, token, token_expires_at, device_id, is_active, ...)
settings(key PK, value)
knowledge_docs(id, filename, file_type, size_bytes, chunk_count, data BYTEA, created_at)
knowledge_chunks(id, doc_id→knowledge_docs CASCADE, content, created_at)
  INDEX GIN (to_tsvector('simple', content))
schema_migrations(version PK, applied_at)
```

---

## Konfigurasi yang Tersimpan di DB

Tabel `settings` (menang atas `.env`):

| Key | Fungsi |
|---|---|
| `ai_provider` | Penyedia LLM |
| `ai_base_url` | Base URL chat/completions |
| `ai_api_key` | API key |
| `ai_model` | Nama model |
| `ai_enabled` | `1`/`0` |

Cache pengaturan AI di `ai.Service` selama 60 detik; `SaveSettings` meng-invalidate cache.

---

## Keamanan Sesion

- Login dashboard → cookie `admin_session = <user>.<HMAC-SHA256(user)>` (`SESSION_SECRET`).
- `requireAuth` membandingkan nilai cookie dengan hasil HMAC — stateless, tanpa penyimpanan sesi.
- Cookie `HttpOnly`, `SameSite=Lax`, path `/`.
