package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-agent/internal/store"
)

// transitions — какие смены статуса разрешены.
//
//	inbox   → backlog (принять) или rejected (отклонить)
//	backlog → done (закрыть)
//	done    → backlog (вернуть в работу)
var transitions = map[string][]string{
	store.StatusInbox:   {store.StatusBacklog, store.StatusRejected},
	store.StatusBacklog: {store.StatusDone},
	store.StatusDone:    {store.StatusBacklog},
}

// TasksService — списки задач и карточка задачи.
type TasksService struct {
	store *store.Store
	clock Clock
}

// NewTasksService создаёт сервис задач.
func NewTasksService(s *store.Store, clock Clock) *TasksService {
	return &TasksService{store: s, clock: clock}
}

// List возвращает задачи со статусом status (inbox, backlog или done).
// Непустой tagID оставляет только задачи с этим тегом.
func (s *TasksService) List(ctx context.Context, status, tagID string) ([]TaskView, error) {
	switch status {
	case store.StatusInbox, store.StatusBacklog, store.StatusDone:
	default:
		return nil, fmt.Errorf("неизвестный список задач %q", status)
	}
	tasks, err := s.store.ListTasks(ctx, store.TaskFilter{Statuses: []string{status}, TagID: tagID})
	if err != nil {
		return nil, err
	}
	return taskViews(ctx, s.store, s.clock, tasks)
}

// Get возвращает задачу для карточки.
func (s *TasksService) Get(ctx context.Context, id string) (TaskDetail, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return TaskDetail{}, userError(err)
	}
	return s.detail(ctx, t)
}

func (s *TasksService) detail(ctx context.Context, t store.Task) (TaskDetail, error) {
	tags, err := s.store.TaskTags(ctx, []string{t.ID})
	if err != nil {
		return TaskDetail{}, err
	}
	links, err := s.store.TaskLinks(ctx, []string{t.ID})
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{
		TaskView:      taskView(s.clock, t, tags[t.ID], links[t.ID]),
		ExternalID:    t.ExternalID,
		Note:          t.Note,
		TitleEditable: t.Source == store.SourceOwn,
	}, nil
}

// fromInput проверяет поля карточки и переносит их в задачу.
func fromTaskInput(t store.Task, in TaskInput) (store.Task, []store.Link, error) {
	links, err := cleanLinks(in.Links, "Ссылка")
	if err != nil {
		return store.Task{}, nil, err
	}
	due := strings.TrimSpace(in.DueDate)
	if due != "" {
		if _, err := time.Parse(store.DateLayout, due); err != nil {
			return store.Task{}, nil, errors.New("укажите срок в виде даты")
		}
	}
	if t.Source == "" || t.Source == store.SourceOwn {
		// У внешних записей название приходит из источника.
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return store.Task{}, nil, errors.New("введите название задачи")
		}
		t.Title = title
	}
	t.Note = in.Note
	t.DueDate = due
	return t, links, nil
}

// Create создаёт свою задачу. Она сразу попадает в бэклог.
func (s *TasksService) Create(ctx context.Context, in TaskInput) (TaskDetail, error) {
	t, links, err := fromTaskInput(store.Task{Kind: store.KindTask, Source: store.SourceOwn, Status: store.StatusBacklog}, in)
	if err != nil {
		return TaskDetail{}, err
	}
	saved, err := s.store.SaveTask(ctx, t, in.TagIDs, links)
	if err != nil {
		return TaskDetail{}, userError(err)
	}
	return s.detail(ctx, saved)
}

// Update меняет задачу. Статус меняется отдельно, через SetStatus.
func (s *TasksService) Update(ctx context.Context, id string, in TaskInput) (TaskDetail, error) {
	current, err := s.store.GetTask(ctx, id)
	if err != nil {
		return TaskDetail{}, userError(err)
	}
	t, links, err := fromTaskInput(current, in)
	if err != nil {
		return TaskDetail{}, err
	}
	saved, err := s.store.SaveTask(ctx, t, in.TagIDs, links)
	if err != nil {
		return TaskDetail{}, userError(err)
	}
	return s.detail(ctx, saved)
}

// SetStatus меняет статус задачи: принять или отклонить входящую,
// закрыть задачу из бэклога или вернуть закрытую в работу.
func (s *TasksService) SetStatus(ctx context.Context, id, status string) error {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		return userError(err)
	}
	if t.Status == status {
		return nil
	}
	for _, allowed := range transitions[t.Status] {
		if allowed == status {
			return userError(s.store.SetTaskStatus(ctx, id, status))
		}
	}
	return fmt.Errorf("задачу со статусом %q нельзя перевести в %q", t.Status, status)
}

// Delete удаляет задачу. Удаление мягкое: запись остаётся в базе.
func (s *TasksService) Delete(ctx context.Context, id string) error {
	return userError(s.store.DeleteTask(ctx, id))
}
