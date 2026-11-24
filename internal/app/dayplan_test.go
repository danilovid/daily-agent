package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"daily-agent/internal/store"
)

func TestPlannerSetPlan(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	mustMeeting(t, s, "Планёрка", at(24, 10, 0))
	mustMeeting(t, s, "Ревью", at(24, 14, 30))
	mustMeeting(t, s, "Завтрашняя", at(25, 10, 0))
	a := mustTask(t, s, store.Task{Title: "Ответить Марине", Status: store.StatusBacklog, DueDate: "2026-11-24"})
	b := mustTask(t, s, store.Task{Title: "Заметки к ревью", Status: store.StatusBacklog})
	inbox := mustTask(t, s, store.Task{Title: "Новая просьба", Status: store.StatusInbox})
	p := NewPlanner(s, testClock())

	bad := []PlanInput{
		{Items: nil},
		{Day: "2026-11-23", Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 30}}},
		{Day: "2026-12-31", Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 30}}},
		{Day: "завтра", Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 30}}},
		{Items: []PlanInputItem{{TaskID: inbox.ID, EstimateMin: 30}}},
		{Items: []PlanInputItem{{TaskID: "нет такой", EstimateMin: 30}}},
		{Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 1}}},
		{Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 30}, {TaskID: a.ID, EstimateMin: 30}}},
		{Summary: strings.Repeat("я", PlanMaxSummary+1), Items: []PlanInputItem{{TaskID: a.ID, EstimateMin: 30}}},
	}
	for _, in := range bad {
		if _, err := p.SetPlan(ctx, in, ""); err == nil {
			t.Errorf("ждали ошибку для %+v", in)
		}
	}
	if plan, _ := s.DayPlan(ctx, "2026-11-24"); plan != nil {
		t.Fatal("после ошибок плана быть не должно")
	}

	view, err := p.SetPlan(ctx, PlanInput{Summary: "Начни с ответа Марине: срок сегодня.", Items: []PlanInputItem{
		{TaskID: a.ID, EstimateMin: 30, Reason: "срок сегодня"},
		{TaskID: b.ID, EstimateMin: 90, Reason: "ревью в 14:30"},
	}}, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Day != "2026-11-24" || view.Source != store.PlanSourceAgent || len(view.Items) != 2 || view.Items[0].Task.Title != "Ответить Марине" {
		t.Fatalf("план: %+v", view)
	}
	// Две встречи по 30 минут: свободно 480 − 60.
	if view.TotalMin != 120 || view.MeetingMin != 60 || view.FreeMin != WorkdayMinutes-60 {
		t.Fatalf("время: всего %d, встречи %d, свободно %d", view.TotalMin, view.MeetingMin, view.FreeMin)
	}
	meetings, total, _ := p.meetings(ctx, "2026-11-24")
	if len(meetings) != 2 || total != 60 || meetings[0].Start != "10:00" {
		t.Fatalf("встречи дня: %+v", meetings)
	}

	// План на завтра можно, он не трогает сегодняшний.
	if _, err := p.SetPlan(ctx, PlanInput{Day: "2026-11-25", Items: []PlanInputItem{{TaskID: b.ID, EstimateMin: 60}}}, ""); err != nil {
		t.Fatal(err)
	}
	// «Сегодня» показывает план и пояснение агента как сводку.
	today, err := NewTodayService(s, testClock()).Today(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if today.Plan == nil || len(today.Plan.Items) != 2 || today.Summary == nil || today.Summary.Text != "Начни с ответа Марине: срок сегодня." {
		t.Fatalf("«Сегодня»: %+v / %+v", today.Plan, today.Summary)
	}
	// Утренняя сводка свежее плана — показывается она; новый план свежее — его пояснение.
	s.SetClock(func() time.Time { return fixedNow.Add(time.Hour) })
	s.CreateSummary(ctx, store.Summary{Day: "2026-11-24", Text: "Утренняя сводка"})
	if today, _ = NewTodayService(s, testClock()).Today(ctx, ""); today.Summary.Text != "Утренняя сводка" {
		t.Fatalf("сводка: %+v", today.Summary)
	}
	s.SetClock(func() time.Time { return fixedNow.Add(2 * time.Hour) })
	p.SetPlan(ctx, PlanInput{Summary: "Новый план", Items: []PlanInputItem{{TaskID: b.ID, EstimateMin: 60}}}, "")
	if today, _ = NewTodayService(s, testClock()).Today(ctx, ""); today.Summary.Text != "Новый план" {
		t.Fatalf("сводка после нового плана: %+v", today.Summary)
	}
}

func TestDayPlanServiceManual(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	a := mustTask(t, s, store.Task{Title: "Задача", Status: store.StatusBacklog})
	inbox := mustTask(t, s, store.Task{Title: "Просьба", Status: store.StatusInbox})
	svc := NewDayPlanService(s, testClock())
	if err := svc.Add(ctx, inbox.ID); err == nil {
		t.Fatal("из «Входящих» в план нельзя")
	}
	if err := svc.Add(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	plan, _ := NewPlanner(s, testClock()).Plan(ctx, "")
	if plan == nil || plan.Source != store.PlanSourceManual || len(plan.Items) != 1 {
		t.Fatalf("ручной план: %+v", plan)
	}
	// Выполненная задача остаётся в плане с отметкой.
	s.SetTaskStatus(ctx, a.ID, store.StatusDone)
	plan, _ = NewPlanner(s, testClock()).Plan(ctx, "")
	if len(plan.Items) != 1 || plan.Items[0].Task.Status != store.StatusDone {
		t.Fatalf("выполненная задача: %+v", plan.Items)
	}
	if err := svc.Remove(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if plan, _ = NewPlanner(s, testClock()).Plan(ctx, ""); len(plan.Items) != 0 {
		t.Fatalf("после удаления: %+v", plan.Items)
	}
}
