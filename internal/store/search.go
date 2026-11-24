package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// Виды записей в поисковом индексе.
const (
	SearchKindTask    = "task"
	SearchKindMeeting = "meeting"
)

// SearchHit — найденная запись.
type SearchHit struct {
	Kind string
	ID   string
}

// NormalizeSearchText заменяет «ё» на «е»: так текст лежит в индексе.
func NormalizeSearchText(s string) string {
	return strings.NewReplacer("ё", "е", "Ё", "Е").Replace(s)
}

// ftsQuery превращает строку из поля поиска в запрос FTS5: каждое слово
// ищется по началу, все слова должны встретиться. Знаки препинания и
// операторы FTS5 из ввода отбрасываются.
func ftsQuery(input string) string {
	words := strings.FieldsFunc(NormalizeSearchText(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}

// Search ищет по названиям и заметкам неудалённых задач и встреч.
// Отклонённые задачи не находятся. Результаты — по убыванию релевантности.
func (s *Store) Search(ctx context.Context, input string, limit int) ([]SearchHit, error) {
	q := ftsQuery(input)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT search_index.kind, search_index.ref_id
		FROM search_index
		LEFT JOIN tasks t ON search_index.kind = 'task' AND t.id = search_index.ref_id
		LEFT JOIN meetings m ON search_index.kind = 'meeting' AND m.id = search_index.ref_id
		WHERE search_index MATCH ? AND (
			(search_index.kind = 'task' AND t.id IS NOT NULL AND t.deleted_at IS NULL AND t.status <> 'rejected') OR
			(search_index.kind = 'meeting' AND m.deleted_at IS NULL AND m.id IS NOT NULL))
		ORDER BY rank
		LIMIT ?`, q, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск: %w", err)
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Kind, &h.ID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
