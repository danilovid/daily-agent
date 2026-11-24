-- Полнотекстовый поиск по названиям и заметкам задач и встреч.
--
-- Индекс ведут триггеры. Токенайзер unicode61 сам приводит русские буквы
-- к одному регистру, но не считает «ё» и «е» одной буквой, поэтому в индекс
-- попадает текст с заменой «ё» на «е». Запрос нормализуется так же в коде.
-- Удалённые и отклонённые записи остаются в индексе и отсекаются при запросе.

CREATE VIRTUAL TABLE search_index USING fts5(
    kind UNINDEXED,
    ref_id UNINDEXED,
    title,
    note,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER tasks_search_insert AFTER INSERT ON tasks BEGIN
    INSERT INTO search_index (kind, ref_id, title, note)
    VALUES ('task', new.id,
            replace(replace(new.title, 'ё', 'е'), 'Ё', 'Е'),
            replace(replace(new.note, 'ё', 'е'), 'Ё', 'Е'));
END;

CREATE TRIGGER tasks_search_update AFTER UPDATE OF title, note ON tasks BEGIN
    DELETE FROM search_index WHERE kind = 'task' AND ref_id = old.id;
    INSERT INTO search_index (kind, ref_id, title, note)
    VALUES ('task', new.id,
            replace(replace(new.title, 'ё', 'е'), 'Ё', 'Е'),
            replace(replace(new.note, 'ё', 'е'), 'Ё', 'Е'));
END;

CREATE TRIGGER tasks_search_delete AFTER DELETE ON tasks BEGIN
    DELETE FROM search_index WHERE kind = 'task' AND ref_id = old.id;
END;

CREATE TRIGGER meetings_search_insert AFTER INSERT ON meetings BEGIN
    INSERT INTO search_index (kind, ref_id, title, note)
    VALUES ('meeting', new.id,
            replace(replace(new.title, 'ё', 'е'), 'Ё', 'Е'),
            replace(replace(new.note, 'ё', 'е'), 'Ё', 'Е'));
END;

CREATE TRIGGER meetings_search_update AFTER UPDATE OF title, note ON meetings BEGIN
    DELETE FROM search_index WHERE kind = 'meeting' AND ref_id = old.id;
    INSERT INTO search_index (kind, ref_id, title, note)
    VALUES ('meeting', new.id,
            replace(replace(new.title, 'ё', 'е'), 'Ё', 'Е'),
            replace(replace(new.note, 'ё', 'е'), 'Ё', 'Е'));
END;

CREATE TRIGGER meetings_search_delete AFTER DELETE ON meetings BEGIN
    DELETE FROM search_index WHERE kind = 'meeting' AND ref_id = old.id;
END;

-- Записи, созданные до этой миграции.
INSERT INTO search_index (kind, ref_id, title, note)
SELECT 'task', id,
       replace(replace(title, 'ё', 'е'), 'Ё', 'Е'),
       replace(replace(note, 'ё', 'е'), 'Ё', 'Е')
FROM tasks;

INSERT INTO search_index (kind, ref_id, title, note)
SELECT 'meeting', id,
       replace(replace(title, 'ё', 'е'), 'Ё', 'Е'),
       replace(replace(note, 'ё', 'е'), 'Ё', 'Е')
FROM meetings;
