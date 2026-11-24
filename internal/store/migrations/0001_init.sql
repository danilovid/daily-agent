-- Daily Agent: начальная схема. Модель данных из docs/architecture.md, раздел 5.
--
-- Соглашения:
-- * Идентификаторы — UUID в виде текста.
-- * Время — текст RFC 3339 в UTC с миллисекундами фиксированной ширины
--   (2026-11-24T07:00:00.000Z), поэтому строки можно сравнивать и сортировать.
-- * Даты без времени — текст YYYY-MM-DD.
-- * Удаление мягкое: запись получает deleted_at.
-- * Пара source и external_id уникальна, если external_id задан.

CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL DEFAULT 'task' CHECK (kind IN ('task', 'mr')),
    source      TEXT NOT NULL DEFAULT 'own' CHECK (source IN ('own', 'tracker', 'repo')),
    external_id TEXT,
    title       TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'inbox' CHECK (status IN ('inbox', 'backlog', 'done', 'rejected')),
    due_date    TEXT,
    url         TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT
);
CREATE UNIQUE INDEX tasks_source_external_id ON tasks (source, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX tasks_status ON tasks (status) WHERE deleted_at IS NULL;

CREATE TABLE task_links (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks (id),
    title      TEXT NOT NULL,
    url        TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE INDEX task_links_task ON task_links (task_id, position);

CREATE TABLE tags (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX tags_name ON tags (name) WHERE deleted_at IS NULL;

CREATE TABLE task_tags (
    task_id    TEXT NOT NULL REFERENCES tasks (id),
    tag_id     TEXT NOT NULL REFERENCES tags (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    PRIMARY KEY (task_id, tag_id)
) WITHOUT ROWID;
CREATE INDEX task_tags_tag ON task_tags (tag_id);

CREATE TABLE meetings (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'meetings')),
    external_id  TEXT,
    title        TEXT NOT NULL,
    starts_at    TEXT NOT NULL,
    duration_min INTEGER NOT NULL CHECK (duration_min > 0),
    repeat_rule  TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    deleted_at   TEXT
);
CREATE UNIQUE INDEX meetings_source_external_id ON meetings (source, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX meetings_starts_at ON meetings (starts_at) WHERE deleted_at IS NULL;

CREATE TABLE meeting_links (
    id         TEXT PRIMARY KEY,
    meeting_id TEXT NOT NULL REFERENCES meetings (id),
    title      TEXT NOT NULL,
    url        TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE INDEX meeting_links_meeting ON meeting_links (meeting_id, position);

CREATE TABLE summaries (
    id         TEXT PRIMARY KEY,
    day        TEXT NOT NULL,
    text       TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE INDEX summaries_day ON summaries (day);

CREATE TABLE chats (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL DEFAULT 'user' CHECK (kind IN ('user', 'scheduled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE TABLE sync_state (
    source          TEXT PRIMARY KEY,
    last_started_at TEXT,
    last_success_at TEXT,
    last_error      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE agent_runs (
    id          TEXT PRIMARY KEY,
    command     TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    ok          INTEGER NOT NULL DEFAULT 0 CHECK (ok IN (0, 1)),
    error       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX agent_runs_started_at ON agent_runs (started_at);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
