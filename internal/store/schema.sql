CREATE TABLE IF NOT EXISTS audit_records (
    id          BIGSERIAL PRIMARY KEY,
    time        TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    cluster_id  TEXT NOT NULL,
    before      JSONB,
    after       JSONB,
    result      TEXT NOT NULL,
    error       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         TEXT PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    done        BOOLEAN NOT NULL DEFAULT false,
    status      INT,
    body        BYTEA
);
