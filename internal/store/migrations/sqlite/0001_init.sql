-- Skema SQLite (mode DB_DRIVER=sqlite) — tanpa server, file lokal.
CREATE TABLE IF NOT EXISTS customers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    phone      TEXT NOT NULL UNIQUE,
    jid        TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price       INTEGER NOT NULL DEFAULT 0,
    image_path  TEXT NOT NULL DEFAULT '',
    stock       INTEGER NOT NULL DEFAULT -1,
    is_active   BOOLEAN NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS orders (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_number TEXT NOT NULL UNIQUE,
    customer_id  INTEGER NOT NULL REFERENCES customers(id),
    status       TEXT NOT NULL DEFAULT 'baru',
    total        INTEGER NOT NULL DEFAULT 0,
    address      TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS order_items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id   INTEGER REFERENCES products(id),
    product_name TEXT NOT NULL,
    price        INTEGER NOT NULL,
    qty          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id  INTEGER NOT NULL REFERENCES customers(id),
    direction    TEXT NOT NULL DEFAULT 'in',
    message_type TEXT NOT NULL DEFAULT 'text',
    body         TEXT NOT NULL DEFAULT '',
    media_url    TEXT NOT NULL DEFAULT '',
    wa_message_id TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_customer ON chat_messages(customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS quick_replies (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword   TEXT NOT NULL UNIQUE,
    reply     TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS broadcasts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    message       TEXT NOT NULL,
    segment       TEXT NOT NULL DEFAULT 'all',
    total_targets INTEGER NOT NULL DEFAULT 0,
    sent          INTEGER NOT NULL DEFAULT 0,
    failed        INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at   DATETIME
);

CREATE TABLE IF NOT EXISTS order_sessions (
    customer_id INTEGER PRIMARY KEY REFERENCES customers(id),
    state       TEXT NOT NULL DEFAULT 'idle',
    product_id  INTEGER,
    qty         INTEGER NOT NULL DEFAULT 1,
    address     TEXT NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS wa_accounts (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    username         TEXT NOT NULL UNIQUE,
    password         TEXT NOT NULL,
    token            TEXT NOT NULL DEFAULT '',
    token_expires_at DATETIME,
    device_id        TEXT NOT NULL DEFAULT '',
    is_active        BOOLEAN NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

INSERT INTO settings (key, value) VALUES
    ('store_name', 'Toko Kita'),
    ('welcome_message', 'Halo! Selamat datang di {store_name}. Ketik *menu* untuk melihat katalog produk kami.'),
    ('catalog_intro', 'Berikut katalog {store_name}:\n\n{products}\n\nKetik nomor produk untuk memesan, atau ketik *menu* untuk melihat ulang.')
ON CONFLICT (key) DO NOTHING;

-- Counter nomor order (pengganti sequence Postgres).
CREATE TABLE IF NOT EXISTS order_seq (
    id  INTEGER PRIMARY KEY CHECK (id = 1),
    val INTEGER NOT NULL DEFAULT 0
);
INSERT INTO order_seq (id, val) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;
