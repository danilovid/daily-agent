-- MCP-серверы агента. Свои серверы (source = app) описаны здесь целиком.
-- У серверов из настроек OpenCode пользователя (source = opencode) здесь только
-- то, что меняют в приложении: включён ли и какие инструменты разрешены.
-- Токены хранятся в Связке ключей, сюда не попадают.
CREATE TABLE mcp_servers (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    title      TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('app', 'opencode')),
    type       TEXT NOT NULL DEFAULT 'remote' CHECK (type IN ('remote', 'local')),
    url        TEXT NOT NULL DEFAULT '',
    command    TEXT NOT NULL DEFAULT '[]',
    auth       TEXT NOT NULL DEFAULT 'none' CHECK (auth IN ('oauth', 'token', 'none')),
    enabled    INTEGER NOT NULL DEFAULT 1,
    -- JSON-список разрешённых инструментов. NULL — по умолчанию: только чтение.
    tools      TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX mcp_servers_name ON mcp_servers (name) WHERE deleted_at IS NULL;
