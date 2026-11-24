-- Рабочие и личные чаты. Рабочий чат идёт только в селфхост-модель и может
-- получать агенду. Личный идёт в модель для личных задач, в том числе
-- облачную, и рабочих данных не получает.
ALTER TABLE chats ADD COLUMN purpose TEXT NOT NULL DEFAULT 'work' CHECK (purpose IN ('work', 'personal'));
