# API Reference

Dokumen ini merinci seluruh endpoint HTTP aplikasi.

- Base URL: `http://localhost:8080`
- Semua endpoint dashboard memerlukan cookie sesi (`admin_session`) dari login.
- Format request/response: `application/json` untuk API, `text/html` untuk halaman dashboard.

---

## 1. Health Check

### `GET /health`

Cek apakah server hidup.

**Response 200:**
```json
{ "status": "ok", "time": "2026-08-15T09:00:00+07:00" }
```

---

## 2. Webhook gowa

### `POST /webhook/gowa`

Diterima dari gowa setiap ada event WhatsApp. Server membalas **200 seketika**, lalu memproses di goroutine.

**Header opsional:** `X-Webhook-Secret: <GOWA_WEBHOOK_SECRET>` — wajib cocok bila secret diubah dari default.

**Body (envelope gowa):**
```json
{
  "event": "message",
  "device_id": "device-1",
  "session_id": "session-x",
  "payload": {
    "id": "WA-123",
    "is_from_me": false,
    "from_name": "Budi",
    "from": "628123456789@s.whatsapp.net",
    "chat_id": "628123456789@s.whatsapp.net",
    "body": "menu",
    "image": null,
    "audio": null,
    "video": null,
    "document": null,
    "sticker": null,
    "location": null,
    "contact": null,
    "timestamp": "1723718400"
  }
}
```

**Alur pemrosesan:**
1. Pesan dari grup/status/broadcast dilewati.
2. Nomor diekstrak → customer dibuat/ambil (`customers`).
3. Pesan disimpan ke `chat_messages` (arah `in`).
4. Bila customer diblokir → berhenti.
5. Bila admin (nomor `ADMIN_PHONE`) dan berawalan `/` → perintah admin.
6. Bila cocok alur order / balasan cepat → dibalas sesuai alur.
7. Lainnya → AI (jika aktif) atau balasan default.

**Response:** `200 OK` (body kosong).

---

## 3. Autentikasi Dashboard

### `GET /admin/login`

Halaman login (HTML).

### `POST /admin/login`

Form-encoded: `username` (email), `password`.

- Sukses: `302 → /admin/overview`, set cookie `admin_session` (HttpOnly, SameSite=Lax) berisi ID tenant.
- Gagal: `302 → /admin/login?err=...`.
- Percobaan gagal dibatasi: 5x → terkunci 15 menit per IP.

### `GET /admin/register`

Halaman pendaftaran toko baru (multi-tenant). Nonaktif bila `ALLOW_REGISTRATION=false`.

### `POST /admin/register`

Form-encoded: `email`, `password`, `password_confirm`, `store_name` (opsional).

- Membuat tenant baru (data terisolasi dari toko lain) lalu login otomatis → `302 → /admin/overview`.
- Email sudah dipakai → `302 → /admin/register?err=...`.

### `POST /admin/logout`

Hapus cookie sesi → `302 → /admin/login`.

### Proteksi

Semua route `/admin/*` (selain login/register) memeriksa cookie. Tanpa sesi valid → `302 → /admin/login`. Sesi berisi ID tenant; semua data yang diakses dibatasi ke tenant tersebut (`tenant_id` dari context).

---

## 4. Ringkasan

### `GET /admin/overview`

Halaman ringkasan: statistik hari ini + 8 pesanan terbaru.

---

## 5. Pesanan

### `GET /admin/orders?status=baru`

Daftar pesanan (maks 200). `status` opsional: `baru|diproses|dikirim|selesai|batal`.

### `POST /admin/orders/:id/status`

Form-encoded: `status` (salah satu dari status di atas).

- Ubah status di DB.
- Kirim notifikasi WhatsApp ke customer (async) via `SendOrderStatusUpdate`.
- `302 → /admin/orders?msg=...`

---

## 6. Produk

### `GET /admin/products`

Daftar semua produk (termasuk nonaktif).

### `POST /admin/products`

Tambah produk. Form-encoded: `name` (wajib), `price` (rupiah, titik ribuan boleh), `stock` (kosong = -1 / tak terbatas), `description`, `image_path`, `is_active` (checkbox).

### `POST /admin/products/:id`

Ubah produk (field sama seperti tambah, termasuk `is_active`).

### `POST /admin/products/:id/delete`

Hapus produk.

---

## 7. Pelanggan

### `GET /admin/customers?q=...`

Daftar pelanggan (maks 500). `q` mencari di phone/nama/catatan (ILIKE).

### `POST /admin/customers/:id/status`

Form-encoded: `status` = `active` | `blocked`.

### `GET /admin/customers/:id/chat`

Riwayat percakapan (maks 100 pesan) dalam bentuk bubble.

---

## 8. Broadcast

### `GET /admin/broadcast`

Form + riwayat broadcast (100 terakhir).

### `POST /admin/broadcast`

Form-encoded: `message` (wajib), `segment` (saat ini hanya `all`).

- Menghitung target = pelanggan aktif (bukan admin, tidak diblokir).
- Membuat baris `broadcasts` status `pending`.
- Worker mengirim otomatis (jeda `BROADCAST_DELAY_SECONDS`).

---

## 9. Balasan Cepat

### `GET /admin/replies`

Daftar balasan cepat.

### `POST /admin/replies`

Form-encoded: `keyword` (wajib, unik), `reply` (wajib).

### `POST /admin/replies/:id/toggle`

Aktif/nonaktifkan.

### `POST /admin/replies/:id/delete`

Hapus.

---

## 10. Akun gowa

### `GET /admin/accounts`

Daftar akun + status device (login/terhubung) akun aktif.

### `POST /admin/accounts`

Form-encoded: `username`, `password` (wajib), `device_id` (opsional), `is_active` (checkbox).

- Kredensial diverifikasi ke gowa (`/auth/login` + `/devices`).
- `device_id` kosong → diambil device pertama dari gowa.
- Akun pertama otomatis aktif.

### `POST /admin/accounts/:id/active`

Jadikan akun ini satu-satunya akun aktif.

### `POST /admin/accounts/:id/webhook`

Daftarkan `GOWA_WEBHOOK_URL` + secret ke device gowa (event `message,message.ack`).

### `GET /admin/accounts/:id/qr`

Halaman QR login (dari gowa `/devices/:id/login`).

---

## 11. AI & Knowledge Base

### `GET /admin/ai`

Form konfigurasi AI + daftar dokumen knowledge base.

### `POST /admin/ai`

Form-encoded: `provider` (wajib), `api_key`, `base_url`, `model`, `enabled` (`1`).

Penyedia preset (base URL & model default otomatis bila dikosongkan):

| provider | Base URL | Model |
|---|---|---|
| `deepseek` | `https://api.deepseek.com/v1` | `deepseek-chat` |
| `groq` | `https://api.groq.com/openai/v1` | `llama-3.3-70b-versatile` |
| `nim` | `https://integrate.api.nvidia.com/v1` | `meta/llama-3.3-70b-instruct` |
| `gemini` | `https://generativelanguage.googleapis.com/v1beta/openai` | `gemini-2.0-flash` |
| `custom` | bebas | bebas |

### `POST /admin/ai/test`

Mengirim prompt kecil ke LLM untuk memverifikasi koneksi. `302 → /admin/ai?msg=...` (sukses/gagal).

### `POST /admin/ai/knowledge`

Upload dokumen — **multipart/form-data**, field `file`.

- Format: `.pdf`, `.csv`, `.xlsx`, `.txt`, `.md` (maks 25 MB).
- Teks diekstrak → di-chunk (~600 karakter) → diindeks.
- Baris CSV/XLSX diubah self-describing: `nama: X | harga: Y | ...`.

### `POST /admin/ai/knowledge/:id/delete`

Hapus dokumen + chunk-nya (cascade).

---

## 12. Aset Statis

### `GET /admin/static/*`

File statis (CSS Tailwind gowa). `Content-Type` sesuai ekstensi.
