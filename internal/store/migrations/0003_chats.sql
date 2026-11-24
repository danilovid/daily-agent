-- Чат приложения ссылается на одну сессию OpenCode.
CREATE UNIQUE INDEX chats_session_id ON chats (session_id);
CREATE INDEX chats_updated_at ON chats (updated_at) WHERE deleted_at IS NULL;
