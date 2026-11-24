-- План на день: задачи бэклога, которые автор делает в этот день, с оценкой
-- и причиной выбора. Составляет агент или сам автор. Новый план на тот же
-- день заменяет прежний, прежний остаётся мягко удалённым.
CREATE TABLE day_plans (
    id         TEXT PRIMARY KEY,
    day        TEXT NOT NULL,
    summary    TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL CHECK (source IN ('agent', 'manual')),
    session_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX day_plans_day ON day_plans (day) WHERE deleted_at IS NULL;

CREATE TABLE day_plan_items (
    plan_id      TEXT NOT NULL REFERENCES day_plans (id),
    task_id      TEXT NOT NULL REFERENCES tasks (id),
    position     INTEGER NOT NULL,
    estimate_min INTEGER NOT NULL DEFAULT 0,
    reason       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (plan_id, task_id)
);
