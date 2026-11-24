package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"daily-agent/internal/store"
)

// callTool вызывает один инструмент своего сервера и возвращает разобранный
// ответ, его текст и признак ошибки.
func callTool(t *testing.T, srv *MCPServer, name, args string) (map[string]any, string, bool) {
	t.Helper()
	text, isErr := toolText(t, mcpCall(t, srv, callMsg(1, name, args))[0])
	var out map[string]any
	if !isErr {
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("%s: ответ не JSON: %s", name, text)
		}
	}
	return out, text, isErr
}

func mustCall(t *testing.T, srv *MCPServer, name, args string) map[string]any {
	t.Helper()
	out, text, isErr := callTool(t, srv, name, args)
	if isErr {
		t.Fatalf("%s %s: %s", name, args, text)
	}
	return out
}

func wantToolError(t *testing.T, srv *MCPServer, name, args, want string) {
	t.Helper()
	_, text, isErr := callTool(t, srv, name, args)
	if !isErr || !strings.Contains(text, want) {
		t.Fatalf("%s %s: ждали ошибку «%s», получили %q (ошибка: %v)", name, args, want, text, isErr)
	}
}

func TestMCPServerToolList(t *testing.T) {
	want := "daily_get_day,daily_list_tasks,daily_set_day_plan,daily_list_meetings,daily_create_meeting," +
		"daily_update_meeting,daily_delete_meeting,daily_create_task,daily_update_task"
	if strings.Join(InternalMCPTools, ",") != want {
		t.Fatalf("инструменты: %v", InternalMCPTools)
	}
	// Инструменты записи помечены для внешних агентов, удаление — как разрушающее.
	for _, tool := range mcpTools {
		ann := tool["annotations"].(map[string]any)
		name := tool["name"].(string)
		read := strings.HasPrefix(name, "get_") || strings.HasPrefix(name, "list_")
		if ann["readOnlyHint"] != read {
			t.Fatalf("%s: readOnlyHint %v", name, ann["readOnlyHint"])
		}
		if (name == "delete_meeting") != (ann["destructiveHint"] == true) {
			t.Fatalf("%s: destructiveHint %v", name, ann["destructiveHint"])
		}
	}
}

func TestMCPServerMeetings(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	srv := NewMCPServer(s, testClock())

	// Встреча из приглашения: местное время, ссылка на созвон.
	created := mustCall(t, srv, "create_meeting",
		`{"title":"Ежедневная встреча","date":"2026-11-25","start":"11:30","duration_min":30,"links":[{"title":"Созвон","url":"https://call.example.test/room-1"}]}`)
	meeting := created["meeting"].(map[string]any)
	id := meeting["id"].(string)
	if created["created"] != true || meeting["start"] != "11:30" || meeting["date"] != "2026-11-25" || meeting["source"] != store.MeetingSourceManual {
		t.Fatalf("создание: %v", created)
	}
	saved, err := s.GetMeeting(ctx, id)
	if err != nil || !saved.StartsAt.Equal(at(25, 11, 30)) || saved.DurationMin != 30 {
		t.Fatalf("в базе: %+v %v", saved, err)
	}
	links, _ := s.MeetingLinks(ctx, []string{id})
	if len(links[id]) != 1 || links[id][0].URL != "https://call.example.test/room-1" {
		t.Fatalf("ссылки: %+v", links[id])
	}

	// Повтор того же приглашения не создаёт вторую встречу.
	wantToolError(t, srv, "create_meeting", `{"title":"  ежедневная   встреча ","date":"2026-11-25","start":"11:30"}`, "уже есть")
	// Ошибки агента — ошибки инструмента, а не протокола.
	wantToolError(t, srv, "create_meeting", `{"title":"Созвон","time":"11:30"}`, "unknown field")
	wantToolError(t, srv, "create_meeting", `{"title":"Созвон","start":"11:30","links":[{"url":"javascript:alert(1)"}]}`, "https://")
	wantToolError(t, srv, "create_meeting", `{"id":"x","title":"Созвон","start":"11:30"}`, "id не нужен")
	wantToolError(t, srv, "create_meeting", `{"title":"Созвон","start":"11:30","repeat":"FREQ=YEARLY"}`, "повтор")
	wantToolError(t, srv, "create_meeting", `{"title":"Созвон","start":"полдень"}`, "время начала")
	wantToolError(t, srv, "create_meeting", `{"title":"`+strings.Repeat("я", AgentMaxTitle+1)+`","start":"11:30"}`, "длиннее")

	// Без даты — сегодня, длительность по умолчанию 30 минут.
	today := mustCall(t, srv, "create_meeting", `{"title":"Разбор","start":"15:00","note":"Подготовить вопросы"}`)["meeting"].(map[string]any)
	if today["date"] != "2026-11-24" || today["duration_min"] != float64(30) {
		t.Fatalf("встреча на сегодня: %v", today)
	}

	// Идентификаторы видны в дне и в списке встреч.
	day := mustCall(t, srv, "get_day", `{"date":"2026-11-25"}`)
	if ms := day["meetings"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != id || day["utc_offset"] != "+03:00" {
		t.Fatalf("день: %v", day)
	}
	list := mustCall(t, srv, "list_meetings", `{"from":"2026-11-24","to":"2026-11-25"}`)
	if ms := list["meetings"].([]any); len(ms) != 2 || ms[1].(map[string]any)["id"] != id || list["utc_offset"] != "+03:00" {
		t.Fatalf("список: %v", list)
	}
	wantToolError(t, srv, "list_meetings", `{"from":"2026-11-25","to":"2026-11-24"}`, "раньше")
	wantToolError(t, srv, "list_meetings", `{"from":"2026-11-01","to":"2026-12-31"}`, "длиннее")

	// Изменение: меняются только переданные поля, ссылки и заметка остаются.
	updated := mustCall(t, srv, "update_meeting", `{"id":"`+id+`","start":"12:00","note":"Перенесли"}`)["meeting"].(map[string]any)
	if updated["start"] != "12:00" || updated["title"] != "Ежедневная встреча" || len(updated["links"].([]any)) != 1 || updated["note"] != "Перенесли" {
		t.Fatalf("изменение: %v", updated)
	}
	wantToolError(t, srv, "update_meeting", `{"id":"`+id+`"}`, "нечего менять")
	wantToolError(t, srv, "update_meeting", `{"id":"нет-такой","start":"12:00"}`, "нет встречи")

	// Повторяющаяся встреча: вся серия меняется и удаляется только с series.
	series := mustCall(t, srv, "create_meeting", `{"title":"Стендап","start":"10:00","repeat":"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"}`)["meeting"].(map[string]any)
	sid := series["id"].(string)
	wantToolError(t, srv, "update_meeting", `{"id":"`+sid+`","start":"10:15"}`, "series: true")
	if got := mustCall(t, srv, "update_meeting", `{"id":"`+sid+`","start":"10:15","series":true}`)["meeting"].(map[string]any); got["start"] != "10:15" {
		t.Fatalf("серия: %v", got)
	}
	if ms := mustCall(t, srv, "list_meetings", `{"from":"2026-11-25","to":"2026-11-27"}`)["meetings"].([]any); len(ms) != 4 {
		t.Fatalf("серия по дням: %v", ms)
	}
	wantToolError(t, srv, "delete_meeting", `{"id":"`+sid+`"}`, "series: true")
	if out := mustCall(t, srv, "delete_meeting", `{"id":"`+sid+`","series":true}`); out["deleted"] != true || out["series"] != true {
		t.Fatalf("удаление серии: %v", out)
	}

	// Встреча из сервиса видеовстреч: только заметка и ссылки, удалить нельзя.
	ext, err := s.CreateMeeting(ctx, store.Meeting{Source: store.MeetingSourceMeetings, ExternalID: "ext-1", Title: "Синк", StartsAt: at(25, 16, 0), DurationMin: 30})
	if err != nil {
		t.Fatal(err)
	}
	wantToolError(t, srv, "update_meeting", `{"id":"`+ext.ID+`","start":"17:00"}`, "только заметка")
	if got := mustCall(t, srv, "update_meeting", `{"id":"`+ext.ID+`","note":"Взять отчёт"}`)["meeting"].(map[string]any); got["note"] != "Взять отчёт" || got["start"] != "16:00" {
		t.Fatalf("заметка внешней встречи: %v", got)
	}
	wantToolError(t, srv, "delete_meeting", `{"id":"`+ext.ID+`"}`, "нельзя")

	// Удаление мягкое: встреча пропадает из списков, запись остаётся.
	if out := mustCall(t, srv, "delete_meeting", `{"id":"`+id+`"}`); out["deleted"] != true || out["title"] != "Ежедневная встреча" {
		t.Fatalf("удаление: %v", out)
	}
	if _, err := s.GetMeeting(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("удалённая встреча: %v", err)
	}
	var deletedAt *string
	if err := s.DB().QueryRowContext(ctx, `SELECT deleted_at FROM meetings WHERE id = ?`, id).Scan(&deletedAt); err != nil || deletedAt == nil {
		t.Fatalf("мягкое удаление: %v %v", deletedAt, err)
	}
	wantToolError(t, srv, "delete_meeting", `{"id":"`+id+`"}`, "нет встречи")
}

func TestMCPServerTasks(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	srv := NewMCPServer(s, testClock())

	created := mustCall(t, srv, "create_task", `{"title":"Подготовить отчёт","due_date":"2026-11-26","note":"Цифры за неделю","links":[{"url":"https://tracker.example.test/T-1"}]}`)
	task := created["task"].(map[string]any)
	id := task["id"].(string)
	if task["status"] != store.StatusBacklog || task["source"] != store.SourceOwn || task["due_date"] != "2026-11-26" || len(task["links"].([]any)) != 1 {
		t.Fatalf("создание: %v", created)
	}
	wantToolError(t, srv, "create_task", `{"title":"подготовить  ОТЧЁТ"}`, "уже есть")
	wantToolError(t, srv, "create_task", `{"title":"Закрытая","status":"done"}`, "backlog или inbox")
	wantToolError(t, srv, "create_task", `{"title":"Срок словами","due_date":"завтра"}`, "срок")
	wantToolError(t, srv, "create_task", `{"title":"Опечатка","due":"2026-11-26"}`, "unknown field")
	inbox := mustCall(t, srv, "create_task", `{"title":"Разобрать письмо","status":"inbox"}`)["task"].(map[string]any)
	if inbox["status"] != store.StatusInbox {
		t.Fatalf("во «Входящие»: %v", inbox)
	}

	// Изменение заметки не трогает теги, ссылки и срок.
	tag, err := s.CreateTag(ctx, store.Tag{Name: "отчёты"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTaskTag(ctx, id, tag.ID); err != nil {
		t.Fatal(err)
	}
	got := mustCall(t, srv, "update_task", `{"id":"`+id+`","note":"Цифры за месяц"}`)["task"].(map[string]any)
	if got["note"] != "Цифры за месяц" || got["due_date"] != "2026-11-26" || len(got["tags"].([]any)) != 1 || len(got["links"].([]any)) != 1 {
		t.Fatalf("заметка: %v", got)
	}
	// Пустой срок убирает срок.
	if got := mustCall(t, srv, "update_task", `{"id":"`+id+`","due_date":""}`)["task"].(map[string]any); got["due_date"] != nil {
		t.Fatalf("срок не убран: %v", got)
	}

	// Статусы — по правилам окна. Неверный переход не записывает и поля.
	wantToolError(t, srv, "update_task", `{"id":"`+id+`","title":"Другое","status":"rejected"}`, "нельзя перевести")
	if cur, _ := s.GetTask(ctx, id); cur.Title != "Подготовить отчёт" {
		t.Fatalf("название записалось при ошибке: %+v", cur)
	}
	if got := mustCall(t, srv, "update_task", `{"id":"`+id+`","status":"done"}`)["task"].(map[string]any); got["status"] != store.StatusDone {
		t.Fatalf("закрыть: %v", got)
	}
	if done := mustCall(t, srv, "list_tasks", `{"status":"done"}`); !strings.Contains(mustJSON(t, done), id) {
		t.Fatalf("закрытые: %v", done)
	}
	if got := mustCall(t, srv, "update_task", `{"id":"`+id+`","status":"backlog","title":"Подготовить отчёт за месяц"}`)["task"].(map[string]any); got["status"] != store.StatusBacklog || got["title"] != "Подготовить отчёт за месяц" {
		t.Fatalf("вернуть в работу: %v", got)
	}
	if got := mustCall(t, srv, "update_task", `{"id":"`+inbox["id"].(string)+`","status":"rejected"}`)["task"].(map[string]any); got["status"] != store.StatusRejected {
		t.Fatalf("отклонить входящую: %v", got)
	}
	wantToolError(t, srv, "update_task", `{"id":"`+id+`"}`, "нечего менять")
	wantToolError(t, srv, "update_task", `{"id":"нет-такой","note":"x"}`, "нет задачи")

	// У задачи из трекера название не меняется, заметка — меняется.
	ext := mustTask(t, s, store.Task{Source: store.SourceTracker, ExternalID: "T-2", Title: "Задача трекера", Status: store.StatusBacklog})
	wantToolError(t, srv, "update_task", `{"id":"`+ext.ID+`","title":"Своё название"}`, "в источнике")
	if got := mustCall(t, srv, "update_task", `{"id":"`+ext.ID+`","note":"Спросить про сроки"}`)["task"].(map[string]any); got["note"] != "Спросить про сроки" || got["title"] != "Задача трекера" {
		t.Fatalf("заметка задачи трекера: %v", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
