CREATE TABLE IF NOT EXISTS customers (
    id         BIGSERIAL PRIMARY KEY,
    phone      TEXT NOT NULL UNIQUE,
    jid        TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS products (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price       BIGINT NOT NULL DEFAULT 0,
    image_path  TEXT NOT NULL DEFAULT '',
    stock       INT NOT NULL DEFAULT -1,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS orders (
    id           BIGSERIAL PRIMARY KEY,
    order_number TEXT NOT NULL UNIQUE,
    customer_id  BIGINT NOT NULL REFERENCES customers(id),
    status       TEXT NOT NULL DEFAULT 'baru',
    total        BIGINT NOT NULL DEFAULT 0,
    address      TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS order_items (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id   BIGINT REFERENCES products(id),
    product_name TEXT NOT NULL,
    price        BIGINT NOT NULL,
    qty          INT NOT NULL
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id           BIGSERIAL PRIMARY KEY,
    customer_id  BIGINT NOT NULL REFERENCES customers(id),
    direction    TEXT NOT NULL DEFAULT 'in',
    message_type TEXT NOT NULL DEFAULT 'text',
    body         TEXT NOT NULL DEFAULT '',
    media_url    TEXT NOT NULL DEFAULT '',
    wa_message_id TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_customer ON chat_messages(customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS quick_replies (
    id        BIGSERIAL PRIMARY KEY,
    keyword   TEXT NOT NULL UNIQUE,
    reply     TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS broadcasts (
    id            BIGSERIAL PRIMARY KEY,
    message       TEXT NOT NULL,
    segment       TEXT NOT NULL DEFAULT 'all',
    total_targets INT NOT NULL DEFAULT 0,
    sent          INT NOT NULL DEFAULT 0,
    failed        INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS order_sessions (
    customer_id BIGINT PRIMARY KEY REFERENCES customers(id),
    state       TEXT NOT NULL DEFAULT 'idle',
    product_id  BIGINT,
    qty         INT NOT NULL DEFAULT 1,
    address     TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_accounts (
    id               BIGSERIAL PRIMARY KEY,
    username         TEXT NOT NULL UNIQUE,
    password         TEXT NOT NULL,
    token            TEXT NOT NULL DEFAULT '',
    token_expires_at TIMESTAMPTZ,
    device_id        TEXT NOT NULL DEFAULT '',
    is_active        BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
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

CREATE SEQUENCE IF NOT EXISTS order_number_seq;