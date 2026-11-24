package agent

import (
	"strings"
	"unicode"
)

// В версии 0.1 агент только читает рабочие системы. Инструмент считается
// инструментом чтения, если его имя начинается со слова чтения и в нём нет
// слов записи. Это защита по имени: аннотации MCP OpenCode агенту не передаёт.
var (
	readWords  = []string{"get", "list", "search", "read", "find", "query", "fetch", "view", "show", "describe", "count", "lookup", "browse", "retrieve"}
	writeWords = []string{
		"create", "update", "delete", "remove", "add", "set", "post", "send", "write", "edit", "merge", "close",
		"assign", "comment", "approve", "move", "archive", "publish", "upload", "put", "patch", "insert", "modify",
		"change", "rename", "reply", "react", "invite", "cancel", "reopen", "transition", "label", "star", "fork",
		"push", "commit", "run", "execute", "trigger", "deploy", "start", "stop", "restart", "mark", "submit", "save",
		"import", "export", "sync", "share", "pin", "unpin", "lock", "unlock", "resolve", "dismiss", "accept", "reject",
	}
)

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

// ToolIsWrite — инструмент похож на запись: имя начинается с глагола записи
// (create_issue, merge_request_merge) или глагол записи стоит после «and»
// (get_and_delete). Существительные вроде merge в get_merge_request не в счёт.
// tool — имя без сервера.
func ToolIsWrite(tool string) bool {
	words := toolWords(tool)
	for i, w := range words {
		if (i == 0 || words[i-1] == "and") && isWriteWord(w) {
			return true
		}
	}
	return false
}

func isWriteWord(w string) bool {
	for _, bad := range writeWords {
		if w == bad {
			return true
		}
	}
	return false
}

// ToolIsRead — инструмент чтения: первое слово — чтение, слов записи нет.
func ToolIsRead(tool string) bool {
	words := toolWords(tool)
	if len(words) == 0 || ToolIsWrite(tool) {
		return false
	}
	for _, r := range readWords {
		if words[0] == r {
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
