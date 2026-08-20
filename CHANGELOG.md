# Changelog

Semua perubahan penting dicatat di sini. Format mengikuti [Keep a Changelog](https://keepachangelog.com/id-ID/1.1.0/).

## [Unreleased] — branch `multi-tenant`

### Ditambahkan
- **Multi-tenant & multi-user**:
  - Tabel `tenants`; setiap toko punya data terisolasi (produk, pelanggan, pesanan, balasan cepat, broadcast, knowledge base AI, akun WA, pengaturan) lewat `tenant_id`.
  - Login dashboard dengan **email + password** per toko; pendaftaran toko baru di `/admin/register` (`ALLOW_REGISTRATION=false` untuk menutup).
  - Tenant pertama di-seed dari `DASHBOARD_USER`/`DASHBOARD_PASSWORD` **hanya saat database kosong** (tenant yang dihapus tidak dibangkitkan ulang).
  - Webhook WhatsApp diarahkan ke tenant yang benar: signature HMAC dicocokkan dengan secret tiap tenant aktif (device gowa memakai secret khusus per toko).
- **PostgreSQL-only**: dukungan SQLite dihapus total (driver, migrasi, placeholder, `DB_DRIVER`/`SQLITE_PATH`).
- **Alur pesanan**:
  - Niat pesan bahasa alami (`saya mau pesan bakso kering 10 pcs`) langsung memulai alur order; nama produk & jumlah diekstrak otomatis.
  - Cek status pesanan dari chat: `pesanan saya mana?`, `sudah dikirim?` → bot tampilkan order terbaru + status (+ ekspedisi & resi).
  - Konfirmasi pembayaran: `sudah transfer`/`sudah bayar` → bot catat dan kirim notifikasi **KONFIRMASI PEMBAYARAN** ke admin via WhatsApp.
  - Ekspedisi & nomor resi per order (`shipping_courier`, `shipping_resi`) — opsional, kosong untuk barang digital; tampil di dashboard, notifikasi `dikirim`, dan balasan cek status.
  - Informasi pembayaran (rekening `payment_account`, QRIS `payment_qris`) tampil di ringkasan & konfirmasi order.
- **Stok otomatis berkurang** saat order dibuat (transaksional; stok `-1` = tak terbatas tidak berubah; stok kurang → rollback).

### Diperbaiki
- Kebocoran data antar tenant: device gowa wajib unik per tenant; webhook fail-closed (device/signature tak dikenal ditolak, tidak jatuh ke tenant lain).
- Bot tidak merespon setelah pindah ke PG: resolusi tenant kini dari signature webhook, bukan `device_id` (gowa mengirim JID, bukan UUID device).
- `SeedTenant1` tidak menaikkan sequence `tenants_id_seq` → registrasi tenant baru bentrok id=1 (bug Postgres).
- Migrasi PG `0007` tidak menambahkan `tenant_id` ke `order_items`.
- Template `/admin/orders` error (`$.Status`) saat ada pesanan pertama.
- Pesan berniat order dijawab AI secara naratif tanpa membuat order — kini masuk alur order sungguhan.
- `CreateOrder` tidak pernah mengurangi stok.
- Kredensial gowa lama (plaintext) tetap terbaca; penyimpanan baru terenkripsi AES-GCM.

### Keamanan
- Secret webhook per tenant; signature global `.env` tetap diterima untuk kompatibilitas instalasi lama.
- Sesi dashboard per tenant (cookie HMAC + ID tenant + epoch); ganti password membatalkan semua sesi toko tersebut.
