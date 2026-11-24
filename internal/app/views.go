// Package app содержит сервисы, которые вызывает окно.
//
// Сервисы читают и меняют данные в хранилище и отдают их в виде, удобном
// экранам. Время уходит в окно строкой RFC 3339 в местном часовом поясе,
// даты — строкой YYYY-MM-DD, время дня — строкой ЧЧ:ММ.
package app

// TodayView — данные экрана «Сегодня».
type TodayView struct {
	Day     string       `json:"day"`
	Summary *SummaryView `json:"summary"`
	// Plan — план на день; nil — плана нет.
	Plan     *PlanView     `json:"plan"`
	Meetings []MeetingView `json:"meetings"`
	Inbox    []TaskView    `json:"inbox"`
	Backlog  []TaskView    `json:"backlog"`
}

// SummaryView — сводка за день.
type SummaryView struct {
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

// MeetingView — одна встреча в списке. У повторяющейся встречи это одна
// встреча серии: StartsAt — её начало, ID — идентификатор серии.
type MeetingView struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	StartsAt    string     `json:"startsAt"`
	DurationMin int        `json:"durationMin"`
	Repeat      string     `json:"repeat"`
	Links       []LinkView `json:"links"`
}

// LinkView — ссылка.
type LinkView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// TaskView — задача в списке.
type TaskView struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Source    string     `json:"source"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	DueDate   string     `json:"dueDate"`
	URL       string     `json:"url"`
	CreatedAt string     `json:"createdAt"`
	Tags      []TagView  `json:"tags"`
	Links     []LinkView `json:"links"`
}

// TaskDetail — задача в карточке.
type TaskDetail struct {
	TaskView
	ExternalID string `json:"externalId"`
	Note       string `json:"note"`
	// TitleEditable — название можно менять. У внешних записей название
	// приходит из источника.
	TitleEditable bool `json:"titleEditable"`
}

// TaskInput — поля карточки задачи.
type TaskInput struct {
	Title   string      `json:"title"`
	Note    string      `json:"note"`
	DueDate string      `json:"dueDate"`
	TagIDs  []string    `json:"tagIds"`
	Links   []LinkInput `json:"links"`
}

// LinkInput — ссылка из карточки. Пустой ID — новая ссылка.
type LinkInput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// MeetingDetail — встреча в карточке. У повторяющейся встречи Date и Start —
// начало первой встречи серии.
type MeetingDetail struct {
	ID          string     `json:"id"`
	Source      string     `json:"source"`
	Title       string     `json:"title"`
	Date        string     `json:"date"`
	Start       string     `json:"start"`
	DurationMin int        `json:"durationMin"`
	Repeat      string     `json:"repeat"`
	Note        string     `json:"note"`
	Links       []LinkView `json:"links"`
	// Editable — название и время можно менять. У встреч из сервиса
	// видеовстреч они приходят из источника.
	Editable bool `json:"editable"`
}

// MeetingInput — поля карточки встречи.
type MeetingInput struct {
	Title       string      `json:"title"`
	Date        string      `json:"date"`
	Start       string      `json:"start"`
	DurationMin int         `json:"durationMin"`
	Repeat      string      `json:"repeat"`
	Note        string      `json:"note"`
	Links       []LinkInput `json:"links"`
}

// MeetingsView — экран «Встречи».
type MeetingsView struct {
	Period        string           `json:"period"`
	UpcomingCount int              `json:"upcomingCount"`
	Days          []MeetingDayView `json:"days"`
}

// MeetingDayView — встречи одного дня.
type MeetingDayView struct {
	Day      string        `json:"day"`
	Meetings []MeetingView `json:"meetings"`
}

// RepeatOption — вариант повтора в карточке встречи.
type RepeatOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// TagView — тег.
type TagView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// SearchView — результаты поиска.
type SearchView struct {
	Query    string        `json:"query"`
	Tasks    []TaskView    `json:"tasks"`
	Meetings []MeetingView `json:"meetings"`
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
