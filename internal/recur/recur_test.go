package recur

import (
	"testing"
	"time"
	_ "time/tzdata" // часовые пояса не зависят от системы
)

var msk = time.FixedZone("MSK", 3*3600)

func d(day, hour, min int) time.Time {
	return time.Date(2026, 11, day, hour, min, 0, 0, msk)
}

func days(ts []time.Time, loc *time.Location) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.In(loc).Format("01-02 Mon 15:04")
	}
	return out
}

func check(t *testing.T, name string, got []time.Time, loc *time.Location, want ...string) {
	t.Helper()
	g := days(got, loc)
	if len(g) != len(want) {
		t.Errorf("%s: получили %v, ждали %v", name, g, want)
		return
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("%s: получили %v, ждали %v", name, g, want)
			return
		}
	}
}

func mustParse(t *testing.T, s string) Rule {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return r
}

func TestParseAndString(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"FREQ=DAILY":                          "FREQ=DAILY",
		"freq=weekly; byday=fr,mo":            "FREQ=WEEKLY;BYDAY=MO,FR",
		"FREQ=WEEKLY;INTERVAL=2":              "FREQ=WEEKLY;INTERVAL=2",
		"FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR,MO": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=MONTHLY;INTERVAL=1":             "FREQ=MONTHLY",
	}
	for in, want := range cases {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Errorf("Canonical(%q) = %q, %v; ждали %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"DAILY", "FREQ=YEARLY", "FREQ=DAILY;INTERVAL=0", "FREQ=DAILY;INTERVAL=99",
		"FREQ=DAILY;BYDAY=MO", "FREQ=WEEKLY;BYDAY=XX", "INTERVAL=2", "FREQ=DAILY;COUNT=3",
		"FREQ=DAILY;FREQ=WEEKLY",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q): ждали ошибку", bad)
		}
	}
}

func TestOneOff(t *testing.T) {
	start := d(24, 10, 0)
	check(t, "в окне", Occurrences(start, Rule{}, msk, d(24, 0, 0), d(25, 0, 0)), msk, "11-24 Tue 10:00")
	check(t, "вне окна", Occurrences(start, Rule{}, msk, d(25, 0, 0), d(26, 0, 0)), msk)
	check(t, "граница to не входит", Occurrences(start, Rule{}, msk, d(23, 0, 0), start), msk)
}

func TestDaily(t *testing.T) {
	start := d(20, 9, 30) // пятница
	r := mustParse(t, "FREQ=DAILY")
	check(t, "каждый день", Occurrences(start, r, msk, d(23, 0, 0), d(26, 0, 0)), msk,
		"11-23 Mon 09:30", "11-24 Tue 09:30", "11-25 Wed 09:30")
	check(t, "до начала серии ничего", Occurrences(start, r, msk, d(1, 0, 0), d(20, 0, 0)), msk)
	check(t, "окно начинается с середины первого дня", Occurrences(start, r, msk, d(20, 12, 0), d(22, 0, 0)), msk,
		"11-21 Sat 09:30")

	every3 := mustParse(t, "FREQ=DAILY;INTERVAL=3")
	check(t, "раз в три дня", Occurrences(start, every3, msk, d(20, 0, 0), d(30, 0, 0)), msk,
		"11-20 Fri 09:30", "11-23 Mon 09:30", "11-26 Thu 09:30", "11-29 Sun 09:30")
}

func TestWeekly(t *testing.T) {
	start := d(16, 10, 0) // понедельник
	weekdays := mustParse(t, "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR")
	check(t, "будни", Occurrences(start, weekdays, msk, d(20, 0, 0), d(25, 0, 0)), msk,
		"11-20 Fri 10:00", "11-23 Mon 10:00", "11-24 Tue 10:00")

	weekly := mustParse(t, "FREQ=WEEKLY")
	check(t, "каждую неделю в день начала", Occurrences(d(18, 15, 0), weekly, msk, d(1, 0, 0), time.Date(2026, 12, 10, 0, 0, 0, 0, msk)), msk,
		"11-18 Wed 15:00", "11-25 Wed 15:00", "12-02 Wed 15:00", "12-09 Wed 15:00")

	biweekly := mustParse(t, "FREQ=WEEKLY;INTERVAL=2")
	check(t, "раз в две недели", Occurrences(start, biweekly, msk, d(17, 0, 0), time.Date(2026, 12, 31, 0, 0, 0, 0, msk)), msk,
		"11-30 Mon 10:00", "12-14 Mon 10:00", "12-28 Mon 10:00")

	// Серия начинается в среду, BYDAY включает понедельник: первая неделя — только с среды.
	midweek := mustParse(t, "FREQ=WEEKLY;BYDAY=MO,WE")
	check(t, "дни до начала серии пропускаются", Occurrences(d(18, 10, 0), midweek, msk, d(16, 0, 0), d(26, 0, 0)), msk,
		"11-18 Wed 10:00", "11-23 Mon 10:00", "11-25 Wed 10:00")
}

func TestMonthly(t *testing.T) {
	r := mustParse(t, "FREQ=MONTHLY")
	start := time.Date(2026, 10, 31, 11, 0, 0, 0, msk)
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, msk)
	to := time.Date(2027, 4, 1, 0, 0, 0, 0, msk)
	// В ноябре, феврале нет 31-го — эти месяцы пропускаются.
	check(t, "31-е число", Occurrences(start, r, msk, from, to), msk,
		"10-31 Sat 11:00", "12-31 Thu 11:00", "01-31 Sun 11:00", "03-31 Wed 11:00")

	quarterly := mustParse(t, "FREQ=MONTHLY;INTERVAL=3")
	check(t, "раз в квартал, окно далеко от начала", Occurrences(time.Date(2025, 1, 15, 9, 0, 0, 0, msk), quarterly, msk,
		time.Date(2026, 6, 1, 0, 0, 0, 0, msk), time.Date(2027, 1, 1, 0, 0, 0, 0, msk)), msk,
		"07-15 Wed 09:00", "10-15 Thu 09:00")
}

func TestDaylightSavingKeepsWallClock(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	// 29 марта 2026 года в Берлине переводят часы на летнее время.
	start := time.Date(2026, 3, 27, 10, 0, 0, 0, berlin)
	got := Occurrences(start, mustParse(t, "FREQ=DAILY"), berlin, start, time.Date(2026, 3, 31, 0, 0, 0, 0, berlin))
	check(t, "по местному времени", got, berlin,
		"03-27 Fri 10:00", "03-28 Sat 10:00", "03-29 Sun 10:00", "03-30 Mon 10:00")
	if gap := got[3].Sub(got[0]); gap != 71*time.Hour {
		t.Errorf("между первой и четвёртой встречей %v, ждали 71h из-за перевода часов", gap)
	}
}

func TestLimit(t *testing.T) {
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, msk)
	got := Occurrences(start, mustParse(t, "FREQ=DAILY"), msk, start, time.Date(2030, 1, 1, 0, 0, 0, 0, msk))
	if len(got) != MaxOccurrences {
		t.Fatalf("получили %d встреч, ждали предел %d", len(got), MaxOccurrences)
	}
}
