-- Multi-tenant: tabel tenant + tenant_id di semua tabel milik toko.
-- Data lama otomatis menjadi milik tenant 1.

CREATE TABLE tenants (
    id             BIGSERIAL PRIMARY KEY,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    session_epoch  BIGINT NOT NULL DEFAULT 0,
    webhook_secret TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- customers: nomor HP unik per tenant.
ALTER TABLE customers ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE customers DROP CONSTRAINT customers_phone_key;
ALTER TABLE customers ADD CONSTRAINT customers_tenant_phone_key UNIQUE (tenant_id, phone);

ALTER TABLE products ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE orders ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE order_items ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE chat_messages ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE broadcasts ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE knowledge_docs ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE knowledge_chunks ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;

ALTER TABLE quick_replies ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE quick_replies DROP CONSTRAINT quick_replies_keyword_key;
ALTER TABLE quick_replies ADD CONSTRAINT quick_replies_tenant_keyword_key UNIQUE (tenant_id, keyword);

ALTER TABLE settings ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE settings DROP CONSTRAINT settings_pkey;
ALTER TABLE settings ADD PRIMARY KEY (tenant_id, key);

ALTER TABLE order_sessions ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE order_sessions DROP CONSTRAINT order_sessions_pkey;
ALTER TABLE order_sessions ADD PRIMARY KEY (tenant_id, customer_id);

ALTER TABLE wa_accounts ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE wa_accounts DROP CONSTRAINT wa_accounts_username_key;
ALTER TABLE wa_accounts ADD CONSTRAINT wa_accounts_tenant_username_key UNIQUE (tenant_id, username);

ALTER TABLE ai_usage ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_pkey;
ALTER TABLE ai_usage ADD PRIMARY KEY (tenant_id, customer_id, day);

ALTER TABLE orders DROP CONSTRAINT orders_order_number_key;
ALTER TABLE orders ADD CONSTRAINT orders_tenant_number_key UNIQUE (tenant_id, order_number);

-- Counter nomor order per tenant (tabel order_seq belum pernah dibuat di
-- Postgres; CREATE IF NOT EXISTS menjaga instalasi lama sekaligus baru).
CREATE TABLE IF NOT EXISTS order_seq (
    id        BIGINT NOT NULL CHECK (id = 1),
    tenant_id BIGINT NOT NULL DEFAULT 1,
    val       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (id, tenant_id)
);
INSERT INTO order_seq (id, tenant_id, val) VALUES (1, 1, 0) ON CONFLICT DO NOTHING;
