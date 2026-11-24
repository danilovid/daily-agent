package agent

import (
	"strings"
	"unicode"
)

// В версии 0.1 агент только читает рабочие системы. Инструмент считается
// инструментом чтения по имени: аннотации MCP OpenCode агенту не передаёт.
// Имена бывают «сначала глагол» (get_issue, create_issue) и «сначала
// существительное» (issue_details, issue_create), правило понимает оба.
var (
	readWords = []string{"get", "list", "search", "read", "find", "query", "fetch", "view", "show", "describe",
		"count", "lookup", "browse", "retrieve"}
	// readNouns — имена инструментов чтения вида issue_details, issue_comments.
	readNouns = []string{"details", "detail", "info", "status", "current", "recent", "summary", "activities",
		"activity", "comments", "links", "types", "history"}
	writeWords = []string{
		"create", "update", "delete", "remove", "add", "set", "post", "send", "write", "edit", "merge", "close",
		"assign", "comment", "approve", "move", "archive", "unarchive", "publish", "upload", "put", "patch", "insert", "modify",
		"change", "rename", "reply", "react", "invite", "cancel", "reopen", "transition", "label", "star", "unstar", "fork",
		"push", "commit", "run", "execute", "trigger", "deploy", "start", "stop", "restart", "mark", "submit", "save",
		"import", "export", "sync", "share", "pin", "unpin", "lock", "unlock", "resolve", "dismiss", "accept", "reject",
	}
	// ambiguousWrite — глаголы записи, которые бывают и существительными:
	// merge request, comment, commit, pipeline run. В середине имени и после
	// глагола чтения они запись не означают: get_merge_request — чтение.
	ambiguousWrite = []string{"merge", "comment", "commit", "push", "run", "label", "share", "import", "export",
		"mark", "save", "close", "sync", "start", "stop", "move", "post"}
)

func inList(list []string, w string) bool {
	for _, x := range list {
		if x == w {
			return true
		}
	}
	return false
}

// toolWords делит имя инструмента на слова: по «_», «-», «.» и заглавным буквам.
func toolWords(name string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	prev := rune(0)
	for _, r := range name {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			flush()
		case unicode.IsUpper(r) && prev != 0 && unicode.IsLower(prev):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return words
}

// ToolIsWrite — инструмент похож на запись. Глагол записи стоит первым
// (create_issue), последним (issue_create, issue_link_add) или после «and»
// (get_and_delete); однозначный глагол записи — в любом месте
// (issue_change_state). Слово, которое бывает существительным, в середине
// имени и после глагола чтения не в счёт: get_merge_request — чтение.
// tool — имя без сервера.
func ToolIsWrite(tool string) bool {
	words := toolWords(tool)
	if len(words) == 0 {
		return false
	}
	readFirst := inList(readWords, words[0])
	for i, w := range words {
		if !inList(writeWords, w) {
			continue
		}
		if i > 0 && words[i-1] == "and" {
			return true
		}
		ambiguous := inList(ambiguousWrite, w)
		if !ambiguous {
			return true
		}
		if !readFirst && (i == 0 || i == len(words)-1) {
			return true
		}
	}
	return false
}

// ToolIsRead — инструмент чтения: не запись, и в имени есть слово чтения
// (get, list, search…) или существительное чтения (details, comments…).
func ToolIsRead(tool string) bool {
	if ToolIsWrite(tool) {
		return false
	}
	for _, w := range toolWords(tool) {
		if inList(readWords, w) || inList(readNouns, w) {
			return true
		}
	}
	return false
}

// ReadPatterns — шаблоны прав для сервера по умолчанию: инструменты, чьи
// имена начинаются со слов чтения. Точный список сессиям даёт AllowedTools.
func ReadPatterns(server string) []string {
	out := make([]string, 0, len(readWords))
	for _, w := range readWords {
		out = append(out, server+"_"+w+"*")
	}
	return out
}

// ShortToolName — имя инструмента без сервера.
func ShortToolName(server, full string) string { return strings.TrimPrefix(full, server+"_") }
