package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"daily-agent/internal/store"
)

// TagPalette — цвета, из которых выбирается цвет нового тега.
// Первые три — из макетов.
var TagPalette = []string{"#6FB1FF", "#FFB36B", "#8FE0C0", "#FF8A8A", "#C9A7FF", "#F2D06B", "#A1A1A6"}

// MaxTagName — самое длинное название тега.
const MaxTagName = 32

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// TagsService — теги.
type TagsService struct {
	store *store.Store
}

// NewTagsService создаёт сервис тегов.
func NewTagsService(s *store.Store) *TagsService { return &TagsService{store: s} }

// Palette возвращает цвета для нового тега.
func (s *TagsService) Palette() []string { return TagPalette }

// List возвращает все теги.
func (s *TagsService) List(ctx context.Context) ([]TagView, error) {
	tags, err := s.store.Tags(ctx)
	if err != nil {
		return nil, err
	}
	return tagViews(tags), nil
}

// Create добавляет тег. Имя уникально без учёта регистра.
func (s *TagsService) Create(ctx context.Context, name, color string) (TagView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TagView{}, errors.New("введите название тега")
	}
	if utf8.RuneCountInString(name) > MaxTagName {
		return TagView{}, errors.New("название тега длиннее 32 символов")
	}
	if !hexColor.MatchString(color) {
		return TagView{}, errors.New("выберите цвет тега")
	}
	t, err := s.store.CreateTag(ctx, store.Tag{Name: name, Color: strings.ToUpper(color)})
	if errors.Is(err, store.ErrDuplicate) {
		return TagView{}, errors.New("такой тег уже есть")
	}
	if err != nil {
		return TagView{}, err
	}
	return TagView{ID: t.ID, Name: t.Name, Color: t.Color}, nil
}
