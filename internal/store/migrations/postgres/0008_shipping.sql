-- Pengiriman: ekspedisi & nomor resi per order.
-- Opsional — untuk barang digital kedua kolom dibiarkan kosong.
ALTER TABLE orders ADD COLUMN shipping_courier TEXT NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN shipping_resi TEXT NOT NULL DEFAULT '';
