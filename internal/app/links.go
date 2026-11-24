package app

import (
	"context"
	"errors"

	"daily-agent/internal/store"
)

// URLOpener открывает адрес в браузере по умолчанию.
type URLOpener func(url string) error

// LinksService открывает ссылки задач и встреч в браузере.
//
// Окно передаёт только идентификатор ссылки, адрес ядро берёт из базы
// и пропускает только http и https. Само приложение по ссылке не ходит:
// адрес открывает системный браузер.
type LinksService struct {
	store *store.Store
	open  URLOpener
}

// NewLinksService создаёт сервис ссылок.
func NewLinksService(s *store.Store, open URLOpener) *LinksService {
	return &LinksService{store: s, open: open}
}

// OpenMeetingLink открывает ссылку на созвон.
func (s *LinksService) OpenMeetingLink(ctx context.Context, linkID string) error {
	l, err := s.store.MeetingLink(ctx, linkID)
	if err != nil {
		return userError(err)
	}
	return s.openURL(l.URL)
}

// OpenTaskLink открывает ссылку-кнопку задачи.
func (s *LinksService) OpenTaskLink(ctx context.Context, linkID string) error {
	l, err := s.store.TaskLink(ctx, linkID)
	if err != nil {
		return userError(err)
	}
	return s.openURL(l.URL)
}

// OpenTaskSource открывает внешнюю запись в её источнике: задачу в трекере
// или мерж-реквест в репозитории.
func (s *LinksService) OpenTaskSource(ctx context.Context, taskID string) error {
	t, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return userError(err)
	}
	if t.URL == "" {
		return errors.New("у задачи нет ссылки на источник")
	}
	return s.openURL(t.URL)
}

func (s *LinksService) openURL(raw string) error {
	u, err := checkURL(raw)
	if err != nil {
		return err
	}
	if s.open == nil {
		return errors.New("открытие ссылок недоступно")
	}
	return s.open(u)
}
