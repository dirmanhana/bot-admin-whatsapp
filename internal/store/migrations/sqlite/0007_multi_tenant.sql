-- Multi-tenant: tabel tenant + tenant_id di semua tabel milik toko.
-- Data lama otomatis menjadi milik tenant 1.
--
-- SQLite: tabel induk (customers) di-rebuild bersama tabel anaknya
-- (orders, order_items, chat_messages). Migrasi ini berjalan dengan
-- PRAGMA foreign_keys=OFF (lihat internal/store/migrate.go) lalu diakhiri
-- dengan foreign_key_check.

CREATE TABLE tenants (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    session_epoch  INTEGER NOT NULL DEFAULT 0,
    webhook_secret TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- customers: nomor HP unik per tenant.
CREATE TABLE customers_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id  INTEGER NOT NULL DEFAULT 1,
    phone      TEXT NOT NULL,
    jid        TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, phone)
);
INSERT INTO customers_new (id, tenant_id, phone, jid, name, notes, status, created_at, updated_at)
    SELECT id, 1, phone, jid, name, notes, status, created_at, updated_at FROM customers;
DROP TABLE customers;
ALTER TABLE customers_new RENAME TO customers;

-- orders: nomor order unik per tenant.
CREATE TABLE orders_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id     INTEGER NOT NULL DEFAULT 1,
    order_number  TEXT NOT NULL,
    customer_id   INTEGER NOT NULL REFERENCES customers(id),
    status        TEXT NOT NULL DEFAULT 'baru',
    total         INTEGER NOT NULL DEFAULT 0,
    address       TEXT NOT NULL DEFAULT '',
    delivery_type TEXT NOT NULL DEFAULT 'kirim',
    delivery_fee  INTEGER NOT NULL DEFAULT 0,
    note          TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, order_number)
);
INSERT INTO orders_new (id, tenant_id, order_number, customer_id, status, total, address, delivery_type, delivery_fee, note, created_at, updated_at)
    SELECT id, 1, order_number, customer_id, status, total, address, delivery_type, delivery_fee, note, created_at, updated_at FROM orders;
DROP TABLE orders;
ALTER TABLE orders_new RENAME TO orders;

-- order_items.
CREATE TABLE order_items_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id    INTEGER NOT NULL DEFAULT 1,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id   INTEGER REFERENCES products(id),
    product_name TEXT NOT NULL,
    price        INTEGER NOT NULL,
    qty          INTEGER NOT NULL
);
INSERT INTO order_items_new (id, tenant_id, order_id, product_id, product_name, price, qty)
    SELECT id, 1, order_id, product_id, product_name, price, qty FROM order_items;
DROP TABLE order_items;
ALTER TABLE order_items_new RENAME TO order_items;

-- chat_messages.
CREATE TABLE chat_messages_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id     INTEGER NOT NULL DEFAULT 1,
    customer_id   INTEGER NOT NULL REFERENCES customers(id),
    direction     TEXT NOT NULL DEFAULT 'in',
    message_type  TEXT NOT NULL DEFAULT 'text',
    body          TEXT NOT NULL DEFAULT '',
    media_url     TEXT NOT NULL DEFAULT '',
    wa_message_id TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO chat_messages_new (id, tenant_id, customer_id, direction, message_type, body, media_url, wa_message_id, created_at)
    SELECT id, 1, customer_id, direction, message_type, body, media_url, wa_message_id, created_at FROM chat_messages;
DROP TABLE chat_messages;
ALTER TABLE chat_messages_new RENAME TO chat_messages;
CREATE INDEX IF NOT EXISTS idx_chat_messages_customer ON chat_messages(customer_id, created_at DESC);

-- Sisa tabel cukup ditambah kolom (tanpa konstrain global yang perlu diubah).
ALTER TABLE products ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE broadcasts ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE knowledge_docs ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE knowledge_chunks ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;

-- quick_replies: kata kunci unik per tenant.
CREATE TABLE quick_replies_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id  INTEGER NOT NULL DEFAULT 1,
    keyword    TEXT NOT NULL,
    reply      TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, keyword)
);
INSERT INTO quick_replies_new (id, tenant_id, keyword, reply, is_active, created_at)
    SELECT id, 1, keyword, reply, is_active, created_at FROM quick_replies;
DROP TABLE quick_replies;
ALTER TABLE quick_replies_new RENAME TO quick_replies;

-- settings: primary key per tenant.
CREATE TABLE settings_new (
    tenant_id INTEGER NOT NULL DEFAULT 1,
    key       TEXT NOT NULL,
    value     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, key)
);
INSERT INTO settings_new (tenant_id, key, value) SELECT 1, key, value FROM settings;
DROP TABLE settings;
ALTER TABLE settings_new RENAME TO settings;

-- order_sessions: satu sesi per (tenant, pelanggan).
CREATE TABLE order_sessions_new (
    tenant_id     INTEGER NOT NULL DEFAULT 1,
    customer_id   INTEGER NOT NULL REFERENCES customers(id),
    state         TEXT NOT NULL DEFAULT 'idle',
    product_id    INTEGER,
    qty           INTEGER NOT NULL DEFAULT 1,
    address       TEXT NOT NULL DEFAULT '',
    delivery_type TEXT NOT NULL DEFAULT 'kirim',
    items         TEXT NOT NULL DEFAULT '[]',
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, customer_id)
);
INSERT INTO order_sessions_new (tenant_id, customer_id, state, product_id, qty, address, delivery_type, items, updated_at)
    SELECT 1, customer_id, state, product_id, qty, address, delivery_type, items, updated_at FROM order_sessions;
DROP TABLE order_sessions;
ALTER TABLE order_sessions_new RENAME TO order_sessions;

-- wa_accounts: username unik per tenant.
CREATE TABLE wa_accounts_new (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id        INTEGER NOT NULL DEFAULT 1,
    username         TEXT NOT NULL,
    password         TEXT NOT NULL,
    token            TEXT NOT NULL DEFAULT '',
    token_expires_at DATETIME,
    device_id        TEXT NOT NULL DEFAULT '',
    is_active        BOOLEAN NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, username)
);
INSERT INTO wa_accounts_new (id, tenant_id, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at)
    SELECT id, 1, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at FROM wa_accounts;
DROP TABLE wa_accounts;
ALTER TABLE wa_accounts_new RENAME TO wa_accounts;

-- ai_usage: penghitung per (tenant, pelanggan, hari).
CREATE TABLE ai_usage_new (
    tenant_id   INTEGER NOT NULL DEFAULT 1,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    day         TEXT NOT NULL,
    count       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, customer_id, day)
);
INSERT INTO ai_usage_new (tenant_id, customer_id, day, count)
    SELECT 1, customer_id, day, count FROM ai_usage;
DROP TABLE ai_usage;
ALTER TABLE ai_usage_new RENAME TO ai_usage;

-- order_seq: counter nomor order per tenant.
CREATE TABLE order_seq_new (
    id        INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL DEFAULT 1,
    val       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id, tenant_id),
    CHECK (id = 1)
);
INSERT INTO order_seq_new (id, tenant_id, val) SELECT id, 1, val FROM order_seq;
DROP TABLE order_seq;
ALTER TABLE order_seq_new RENAME TO order_seq;
