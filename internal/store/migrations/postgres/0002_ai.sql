-- Knowledge base untuk AI (katalog, deskripsi produk, harga, dll.)
CREATE TABLE IF NOT EXISTS knowledge_docs (
    id          BIGSERIAL PRIMARY KEY,
    filename    TEXT NOT NULL,
    file_type   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    chunk_count INT NOT NULL DEFAULT 0,
    data        BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id         BIGSERIAL PRIMARY KEY,
    doc_id     BIGINT NOT NULL REFERENCES knowledge_docs(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_doc ON knowledge_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_tsv
    ON knowledge_chunks USING GIN (to_tsvector('simple', content));
