CREATE TABLE IF NOT EXISTS ai_usage (
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    day         DATE NOT NULL,
    count       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (customer_id, day)
);

ALTER TABLE order_sessions ADD COLUMN items TEXT NOT NULL DEFAULT '[]';
ALTER TABLE orders ADD COLUMN delivery_type TEXT NOT NULL DEFAULT 'kirim';
ALTER TABLE orders ADD COLUMN delivery_fee BIGINT NOT NULL DEFAULT 0;