-- Схема БД сокращателя ссылок (применяется автоматически при первом старте postgres-primary).

CREATE TABLE IF NOT EXISTS links (
    code       TEXT        PRIMARY KEY,          -- короткий код, напр. "k3XnqP"
    url        TEXT        NOT NULL,             -- целевой URL
    clicks     BIGINT      NOT NULL DEFAULT 0,   -- счётчик переходов (обновляет worker)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS link_events (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT        NOT NULL,
    event      TEXT        NOT NULL,             -- тип события, пока 'clicked'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_link_events_code ON link_events (code);
CREATE INDEX IF NOT EXISTS idx_link_events_created_at ON link_events (created_at);
