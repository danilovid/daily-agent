// Package recur разворачивает повторяющиеся встречи.
//
// Правило повтора хранится строкой в духе RRULE из iCalendar, но
// поддерживается только нужное приложению подмножество:
//
//	FREQ=DAILY|WEEKLY|MONTHLY
//	INTERVAL=N        (1–52, по умолчанию 1)
//	BYDAY=MO,TU,...   (только для WEEKLY; по умолчанию — день недели начала)
//
// Повторы считаются по местному времени: встреча в 10:00 остаётся в 10:00
// и после перехода на летнее время.
package recur

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Частоты повтора.
const (
	Daily   = "DAILY"
	Weekly  = "WEEKLY"
	Monthly = "MONTHLY"
)

// MaxOccurrences ограничивает число встреч, которые вернёт один вызов.
const MaxOccurrences = 1000

var dayCodes = map[string]time.Weekday{
	"MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday,
	"FR": time.Friday, "SA": time.Saturday, "SU": time.Sunday,
}

var dayNames = map[time.Weekday]string{
	time.Monday: "MO", time.Tuesday: "TU", time.Wednesday: "WE", time.Thursday: "TH",
	time.Friday: "FR", time.Saturday: "SA", time.Sunday: "SU",
}

// Rule — правило повтора. Нулевое значение — «не повторять».
type Rule struct {
	Freq     string
	Interval int
	ByDay    []time.Weekday
}

// IsZero сообщает, что встреча не повторяется.
func (r Rule) IsZero() bool { return r.Freq == "" }

// Parse разбирает правило. Пустая строка — «не повторять».
func Parse(s string) (Rule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, nil
	}
	r := Rule{Interval: 1}
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return Rule{}, fmt.Errorf("повтор %q: часть %q без «=»", s, part)
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.ToUpper(strings.TrimSpace(value))
		if seen[key] {
			return Rule{}, fmt.Errorf("повтор %q: %s указан дважды", s, key)
		}
		seen[key] = true
		switch key {
		case "FREQ":
			if value != Daily && value != Weekly && value != Monthly {
				return Rule{}, fmt.Errorf("повтор %q: неизвестная частота %q", s, value)
			}
			r.Freq = value
		case "INTERVAL":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 52 {
				return Rule{}, fmt.Errorf("повтор %q: интервал должен быть от 1 до 52", s)
			}
			r.Interval = n
		case "BYDAY":
			for _, code := range strings.Split(value, ",") {
				d, ok := dayCodes[strings.TrimSpace(code)]
				if !ok {
					return Rule{}, fmt.Errorf("повтор %q: неизвестный день %q", s, code)
				}
				r.ByDay = append(r.ByDay, d)
			}
		default:
			return Rule{}, fmt.Errorf("повтор %q: параметр %s не поддерживается", s, key)
		}
	}
	if r.Freq == "" {
		return Rule{}, fmt.Errorf("повтор %q: нет FREQ", s)
	}
	if len(r.ByDay) > 0 && r.Freq != Weekly {
		return Rule{}, errors.New("повтор: BYDAY допустим только с FREQ=WEEKLY")
	}
	r.ByDay = sortedDays(r.ByDay)
	return r, nil
}

// String возвращает правило в каноническом виде.
func (r Rule) String() string {
	if r.IsZero() {
		return ""
	}
	parts := []string{"FREQ=" + r.Freq}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	if len(r.ByDay) > 0 {
		codes := make([]string, len(r.ByDay))
		for i, d := range sortedDays(r.ByDay) {
			codes[i] = dayNames[d]
		}
		parts = append(parts, "BYDAY="+strings.Join(codes, ","))
	}
	return strings.Join(parts, ";")
}

// Canonical разбирает правило и возвращает его каноническую запись.
func Canonical(s string) (string, error) {
	r, err := Parse(s)
	if err != nil {
		return "", err
	}
	return r.String(), nil
}

// sortedDays сортирует дни недели с понедельника и убирает повторы.
func sortedDays(days []time.Weekday) []time.Weekday {
	seen := map[time.Weekday]bool{}
	var out []time.Weekday
	for _, d := range days {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return mondayIndex(out[i]) < mondayIndex(out[j]) })
	return out
}

func mondayIndex(d time.Weekday) int { return (int(d) + 6) % 7 }

// civilDays — число календарных дней от a до b (даты без времени).
func civilDays(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 12, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 12, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}

// Occurrences возвращает начала встреч серии, которые попадают в [from, to).
// start — начало первой встречи серии, loc — часовой пояс, в котором
// считаются дни и часы. Для нулевого правила это сама встреча, если она
// попадает в промежуток.
func Occurrences(start time.Time, r Rule, loc *time.Location, from, to time.Time) []time.Time {
	if loc == nil {
		loc = time.Local
	}
	if !from.Before(to) {
		return nil
	}
	if r.IsZero() {
		if !start.Before(from) && start.Before(to) {
			return []time.Time{start}
		}
		return nil
	}
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}
	s := start.In(loc)
	h, m, sec := s.Clock()
	at := func(y int, mo time.Month, d int) time.Time {
		return time.Date(y, mo, d, h, m, sec, s.Nanosecond(), loc)
	}
	// Шаги до from пропускаем арифметикой, а не перебором.
	fromLocal := from.In(loc)
	var out []time.Time
	add := func(t time.Time) bool {
		if t.Before(start) || t.Before(from) {
			return true
		}
		if !t.Before(to) || len(out) >= MaxOccurrences {
			return false
		}
		out = append(out, t)
		return true
	}

	switch r.Freq {
	case Daily:
		k := 0
		if d := civilDays(s, fromLocal); d > 0 {
			k = d/interval - 1
			if k < 0 {
				k = 0
			}
		}
		for ; ; k++ {
			t := at(s.Year(), s.Month(), s.Day()+k*interval)
			if !add(t) {
				break
			}
		}
	case Weekly:
		days := r.ByDay
		if len(days) == 0 {
			days = []time.Weekday{s.Weekday()}
		}
		// Понедельник недели, в которой начинается серия.
		weekStart := at(s.Year(), s.Month(), s.Day()-mondayIndex(s.Weekday()))
		k := 0
		if d := civilDays(weekStart, fromLocal); d > 0 {
			k = d/(7*interval) - 1
			if k < 0 {
				k = 0
			}
		}
	weeks:
		for ; ; k++ {
			for _, wd := range days {
				t := at(weekStart.Year(), weekStart.Month(), weekStart.Day()+k*7*interval+mondayIndex(wd))
				if !add(t) {
					break weeks
				}
			}
		}
	case Monthly:
		k := 0
		if months := (fromLocal.Year()-s.Year())*12 + int(fromLocal.Month()-s.Month()); months > 0 {
			k = months/interval - 1
			if k < 0 {
				k = 0
			}
		}
		for ; k < 100000; k++ {
			t := at(s.Year(), s.Month()+time.Month(k*interval), s.Day())
			if t.Day() != s.Day() {
				// В месяце нет такого числа, например 31 ноября: пропускаем месяц.
				if t.After(to) {
					break
				}
				continue
			}
			if !add(t) {
				break
			}
		}
	}
	return out
}
