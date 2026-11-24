package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDayPlans(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	a, _ := s.CreateTask(ctx, Task{Title: "Задача А", Status: StatusBacklog})
	b, _ := s.CreateTask(ctx, Task{Title: "Задача Б", Status: StatusBacklog})
	c, _ := s.CreateTask(ctx, Task{Title: "Задача В", Status: StatusBacklog})

	if p, err := s.DayPlan(ctx, "2026-11-25"); err != nil || p != nil {
		t.Fatalf("пустой план: %+v %v", p, err)
	}
	first, err := s.SaveDayPlan(ctx, DayPlan{Day: "2026-11-25", Source: PlanSourceAgent, Summary: "Начни с А", SessionID: "ses_1",
		Items: []DayPlanItem{{TaskID: a.ID, EstimateMin: 60, Reason: "срок сегодня"}, {TaskID: b.ID, EstimateMin: 30}}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.DayPlan(ctx, "2026-11-25")
	if p.ID != first.ID || p.Summary != "Начни с А" || len(p.Items) != 2 || p.Items[0].TaskID != a.ID || p.Items[0].Reason != "срок сегодня" || p.Items[1].EstimateMin != 30 {
		t.Fatalf("план: %+v", p)
	}

	for _, bad := range []DayPlan{
		{Day: "завтра", Source: PlanSourceAgent},
		{Day: "2026-11-25", Source: "other"},
		{Day: "2026-11-25", Source: PlanSourceAgent, Items: []DayPlanItem{{TaskID: a.ID}, {TaskID: a.ID}}},
		{Day: "2026-11-25", Source: PlanSourceAgent, Items: []DayPlanItem{{TaskID: a.ID, EstimateMin: -1}}},
		{Day: "2026-11-25", Source: PlanSourceAgent, Items: []DayPlanItem{{TaskID: "нет такой"}}},
	} {
		if _, err := s.SaveDayPlan(ctx, bad); err == nil {
			t.Errorf("ждали ошибку для %+v", bad)
		}
	}
	// Неудачная запись не трогает прежний план.
	if p, _ := s.DayPlan(ctx, "2026-11-25"); p.ID != first.ID {
		t.Fatal("ошибка записи заменила план")
	}

	// Новый план заменяет прежний, прежний мягко удалён.
	now = now.Add(time.Hour)
	second, _ := s.SaveDayPlan(ctx, DayPlan{Day: "2026-11-25", Source: PlanSourceAgent, Items: []DayPlanItem{{TaskID: c.ID, EstimateMin: 90}}})
	p, _ = s.DayPlan(ctx, "2026-11-25")
	if p.ID != second.ID || len(p.Items) != 1 || p.Items[0].TaskID != c.ID {
		t.Fatalf("новый план: %+v", p)
	}
	var deleted int
	s.DB().QueryRow(`SELECT COUNT(*) FROM day_plans WHERE deleted_at IS NOT NULL`).Scan(&deleted)
	if deleted != 1 {
		t.Fatal("прежний план должен остаться мягко удалённым")
	}

	// Вручную: добавить, не задвоить, убрать.
	if err := s.AddToDayPlan(ctx, "2026-11-25", a.ID, 0); err != nil {
		t.Fatal(err)
	}
	s.AddToDayPlan(ctx, "2026-11-25", a.ID, 0)
	p, _ = s.DayPlan(ctx, "2026-11-25")
	if len(p.Items) != 2 || p.Items[1].TaskID != a.ID || !p.UpdatedAt.Equal(now) {
		t.Fatalf("после добавления: %+v", p)
	}
	s.RemoveFromDayPlan(ctx, "2026-11-25", c.ID)
	if p, _ = s.DayPlan(ctx, "2026-11-25"); len(p.Items) != 1 || p.Items[0].TaskID != a.ID {
		t.Fatalf("после удаления: %+v", p)
	}
	// Плана на другой день нет — добавление создаёт план автора.
	s.AddToDayPlan(ctx, "2026-11-26", b.ID, 15)
	if p, _ = s.DayPlan(ctx, "2026-11-26"); p == nil || p.Source != PlanSourceManual || p.Items[0].EstimateMin != 15 {
		t.Fatalf("ручной план: %+v", p)
	}
	// Удалённая задача из плана пропадает.
	s.DeleteTask(ctx, b.ID)
	if p, _ = s.DayPlan(ctx, "2026-11-26"); len(p.Items) != 0 {
		t.Fatalf("удалённая задача в плане: %+v", p.Items)
	}
}

func TestDataVersionSeesOtherProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	ctx := context.Background()
	app, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	other, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	v1, _ := app.DataVersion(ctx)
	app.CreateTask(ctx, Task{Title: "своя запись"})
	if v2, _ := app.DataVersion(ctx); v2 != v1 {
		t.Fatal("свои записи не должны менять признак")
	}
	other.CreateTask(ctx, Task{Title: "запись другого процесса"})
	if v3, _ := app.DataVersion(ctx); v3 == v1 {
		t.Fatal("запись другого соединения должна менять признак")
	}
}
