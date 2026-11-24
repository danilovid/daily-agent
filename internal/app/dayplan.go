package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"daily-agent/internal/store"
)

// WorkdayMinutes — рабочий день, из которого вычитаются встречи. Допущение:
// восемь часов; позже настраивается в «Общих».
const WorkdayMinutes = 8 * 60

// Ограничения плана от агента.
const (
	PlanMaxItems    = 20
	PlanMinEstimate = 5
	PlanMaxEstimate = WorkdayMinutes
	PlanMaxSummary  = 1000
	PlanMaxReason   = 200
	// PlanDaysAhead — на сколько дней вперёд можно составить план.
	PlanDaysAhead = 14
)

// DayMeeting — встреча в сведениях о дне.
type DayMeeting struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Start       string `json:"start"` // ЧЧ:ММ по местному времени
	DurationMin int    `json:"duration_min"`
}

// PlanItemView — задача в плане на день.
type PlanItemView struct {
	Task        TaskView `json:"task"`
	EstimateMin int      `json:"estimateMin"`
	Reason      string   `json:"reason"`
}

// PlanView — план на день.
type PlanView struct {
	Day     string `json:"day"`
	Summary string `json:"summary"`
	// Source: agent — составил агент, manual — автор.
	Source    string         `json:"source"`
	UpdatedAt string         `json:"updatedAt"`
	Items     []PlanItemView `json:"items"`
	// TotalMin — сумма оценок, FreeMin — рабочий день минус встречи.
	TotalMin   int `json:"totalMin"`
	MeetingMin int `json:"meetingMin"`
	FreeMin    int `json:"freeMin"`
}

// PlanInput — план от агента.
type PlanInput struct {
	Day     string
	Summary string
	Items   []PlanInputItem
}

// PlanInputItem — задача в плане от агента.
type PlanInputItem struct {
	TaskID      string `json:"task_id"`
	EstimateMin int    `json:"estimate_min"`
	Reason      string `json:"reason"`
}

// Planner — план на день: сведения о дне для агента, проверка и запись плана.
type Planner struct {
	store *store.Store
	clock Clock
}

// NewPlanner создаёт планировщик дня.
func NewPlanner(st *store.Store, clock Clock) *Planner { return &Planner{store: st, clock: clock} }

// day приводит день к виду ГГГГ-ММ-ДД. Пусто — сегодня.
func (p *Planner) day(day string) (string, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return p.clock.today(), nil
	}
	if _, _, err := p.clock.dayBounds(day); err != nil {
		return "", fmt.Errorf("день %q: ждали ГГГГ-ММ-ДД", day)
	}
	return day, nil
}

// meetings — встречи дня и их общая длительность в пределах дня.
func (p *Planner) meetings(ctx context.Context, day string) ([]DayMeeting, int, error) {
	start, end, err := p.clock.dayBounds(day)
	if err != nil {
		return nil, 0, err
	}
	occs, err := loadOccurrences(ctx, p.store, p.clock.loc(), start, end)
	if err != nil {
		return nil, 0, err
	}
	out := []DayMeeting{}
	total := 0
	for _, o := range occs {
		out = append(out, DayMeeting{ID: o.meeting.ID, Title: o.meeting.Title, Start: o.start.In(p.clock.loc()).Format("15:04"), DurationMin: o.meeting.DurationMin})
		total += o.meeting.DurationMin
	}
	return out, total, nil
}

// Plan возвращает план на день или nil, если плана нет.
func (p *Planner) Plan(ctx context.Context, day string) (*PlanView, error) {
	day, err := p.day(day)
	if err != nil {
		return nil, err
	}
	plan, err := p.store.DayPlan(ctx, day)
	if err != nil || plan == nil {
		return nil, err
	}
	_, meetingMin, err := p.meetings(ctx, day)
	if err != nil {
		return nil, err
	}
	view := &PlanView{Day: day, Summary: plan.Summary, Source: plan.Source, UpdatedAt: p.clock.format(plan.UpdatedAt),
		Items: []PlanItemView{}, MeetingMin: meetingMin, FreeMin: max(0, WorkdayMinutes-meetingMin)}
	var tasks []store.Task
	for _, it := range plan.Items {
		t, err := p.store.GetTask(ctx, it.TaskID)
		if err != nil {
			continue // задачу удалили
		}
		tasks = append(tasks, t)
	}
	views, err := taskViews(ctx, p.store, p.clock, tasks)
	if err != nil {
		return nil, err
	}
	byID := map[string]TaskView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	for _, it := range plan.Items {
		v, ok := byID[it.TaskID]
		if !ok {
			continue
		}
		view.Items = append(view.Items, PlanItemView{Task: v, EstimateMin: it.EstimateMin, Reason: it.Reason})
		view.TotalMin += it.EstimateMin
	}
	return view, nil
}

// SetPlan проверяет и записывает план от агента, заменяя прежний на этот
// день. В план попадают только задачи из бэклога: агент планирует, а не
// разбирает «Входящие» и не меняет задачи.
func (p *Planner) SetPlan(ctx context.Context, in PlanInput, sessionID string) (*PlanView, error) {
	day, err := p.day(in.Day)
	if err != nil {
		return nil, err
	}
	today := p.clock.today()
	last, _ := time.Parse(store.DateLayout, today)
	if day < today || day > last.AddDate(0, 0, PlanDaysAhead).Format(store.DateLayout) {
		return nil, fmt.Errorf("план можно составить на сегодня и на %d дней вперёд, а не на %s", PlanDaysAhead, day)
	}
	summary := strings.TrimSpace(in.Summary)
	if utf8.RuneCountInString(summary) > PlanMaxSummary {
		return nil, fmt.Errorf("пояснение длиннее %d символов", PlanMaxSummary)
	}
	if len(in.Items) == 0 {
		return nil, errors.New("в плане нет задач")
	}
	if len(in.Items) > PlanMaxItems {
		return nil, fmt.Errorf("в плане больше %d задач", PlanMaxItems)
	}
	plan := store.DayPlan{Day: day, Summary: summary, Source: store.PlanSourceAgent, SessionID: sessionID}
	seen := map[string]bool{}
	for _, it := range in.Items {
		id := strings.TrimSpace(it.TaskID)
		if seen[id] {
			return nil, fmt.Errorf("задача %s в плане дважды", id)
		}
		seen[id] = true
		t, err := p.store.GetTask(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("нет задачи %s: возьмите идентификатор из списка задач", id)
		}
		if t.Status != store.StatusBacklog {
			return nil, fmt.Errorf("задача %s не в бэклоге: в план берутся только задачи из бэклога", id)
		}
		if it.EstimateMin < PlanMinEstimate || it.EstimateMin > PlanMaxEstimate {
			return nil, fmt.Errorf("оценка задачи %s: от %d до %d минут", id, PlanMinEstimate, PlanMaxEstimate)
		}
		reason := strings.TrimSpace(it.Reason)
		if utf8.RuneCountInString(reason) > PlanMaxReason {
			return nil, fmt.Errorf("причина для задачи %s длиннее %d символов", id, PlanMaxReason)
		}
		plan.Items = append(plan.Items, store.DayPlanItem{TaskID: id, EstimateMin: it.EstimateMin, Reason: reason})
	}
	if _, err := p.store.SaveDayPlan(ctx, plan); err != nil {
		return nil, err
	}
	return p.Plan(ctx, day)
}

// DayPlanService — план на день в окне: добавить задачу вручную и убрать.
type DayPlanService struct {
	planner *Planner
	store   *store.Store
}

// NewDayPlanService создаёт сервис плана для окна.
func NewDayPlanService(st *store.Store, clock Clock) *DayPlanService {
	return &DayPlanService{planner: NewPlanner(st, clock), store: st}
}

// Add добавляет задачу бэклога в план на сегодня.
func (s *DayPlanService) Add(ctx context.Context, taskID string) error {
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return userError(err)
	}
	if t.Status != store.StatusBacklog {
		return errors.New("в план на день попадают задачи из бэклога")
	}
	return s.store.AddToDayPlan(ctx, s.planner.clock.today(), taskID, 0)
}

// Remove убирает задачу из плана на сегодня. Сама задача остаётся в бэклоге.
func (s *DayPlanService) Remove(ctx context.Context, taskID string) error {
	return s.store.RemoveFromDayPlan(ctx, s.planner.clock.today(), taskID)
}
