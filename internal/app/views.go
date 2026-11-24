// Package app содержит сервисы, которые вызывает окно.
//
// Сервисы читают данные из хранилища и отдают их в виде, удобном экранам.
// Время уходит в окно строкой RFC 3339 в местном часовом поясе, даты —
// строкой YYYY-MM-DD.
package app

// TodayView — данные экрана «Сегодня».
type TodayView struct {
	Day      string        `json:"day"`
	Summary  *SummaryView  `json:"summary"`
	Meetings []MeetingView `json:"meetings"`
	Inbox    []TaskView    `json:"inbox"`
	Backlog  []TaskView    `json:"backlog"`
}

// SummaryView — сводка за день.
type SummaryView struct {
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

// MeetingView — встреча в повестке.
type MeetingView struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	StartsAt    string     `json:"startsAt"`
	DurationMin int        `json:"durationMin"`
	Links       []LinkView `json:"links"`
}

// LinkView — ссылка.
type LinkView struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// TaskView — задача в списке.
type TaskView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	DueDate   string    `json:"dueDate"`
	URL       string    `json:"url"`
	CreatedAt string    `json:"createdAt"`
	Tags      []TagView `json:"tags"`
}

// TagView — тег.
type TagView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// SidebarView — данные боковой панели.
type SidebarView struct {
	Today       string        `json:"today"`
	Month       string        `json:"month"`
	Counts      SidebarCounts `json:"counts"`
	Tags        []TagView     `json:"tags"`
	MeetingDays []int         `json:"meetingDays"`
}

// SidebarCounts — числа у разделов навигации.
type SidebarCounts struct {
	Inbox         int `json:"inbox"`
	Backlog       int `json:"backlog"`
	MeetingsToday int `json:"meetingsToday"`
	Chats         int `json:"chats"`
}
