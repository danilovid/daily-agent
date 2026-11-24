// Package stubmodel — заглушка модели для разработки и тестов.
//
// Это сервер на 127.0.0.1 с API, совместимым с OpenAI (chat/completions,
// потоковый и обычный ответ). Он не ходит в сеть и не знает рабочих данных:
// на любое сообщение отвечает вымышленным текстом. Строка вида
//
//	[stub:call name=shell args={"command":"touch /tmp/x"}]
//
// в последнем сообщении пользователя заставляет заглушку попросить вызов
// инструмента — так проверяются запреты агента.
package stubmodel

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ModelID — модель, которую показывает заглушка.
const ModelID = "stub-model"

// Reply — ответ заглушки на обычное сообщение.
const Reply = "Тестовый ответ заглушки."

// AfterTool — ответ заглушки после результата инструмента.
const AfterTool = "Инструмент отработал или был запрещён."

// SlowMarker в сообщении включает медленный длинный ответ: по слову раз в
// SlowDelay. Так проверяется остановка ответа.
const SlowMarker = "[stub:slow]"

// SlowDelay — пауза между словами медленного ответа.
const SlowDelay = 250 * time.Millisecond

// SlowReply — длинный ответ медленного режима.
var SlowReply = strings.TrimSpace(strings.Repeat("Это длинный медленный ответ заглушки. ", 8))

// Request — то, что заглушка узнала о запросе.
type Request struct {
	Model     string // модель, которую запросил OpenCode
	APIKey    string // ключ из заголовка Authorization; у заглушки он вымышленный
	Stream    bool
	Tools     []string  // инструменты, которые OpenCode предложил модели
	ToolInfo  []ToolDef // они же с описаниями
	LastRole  string
	LastText  string
	ToolCalls []Call // вызовы, которые заглушка попросила в ответ
}

// ToolDef — инструмент, предложенный модели.
type ToolDef struct {
	Name        string
	Description string
}

// Call — вызов инструмента.
type Call struct {
	Name string
	Args string
}

// Server — запущенная заглушка.
type Server struct {
	URL string // адрес с /v1 на конце

	srv      *http.Server
	mu       sync.Mutex
	requests []Request
}

// Start запускает заглушку на свободном порту 127.0.0.1.
func Start() (*Server, error) { return StartAt("127.0.0.1:0") }

// StartAt запускает заглушку по адресу addr. Только на этом Mac: 127.0.0.1
// или localhost.
func StartAt(addr string) (*Server, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if host != "127.0.0.1" && host != "localhost" {
		return nil, fmt.Errorf("заглушка слушает только 127.0.0.1, а не %s", host)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{URL: "http://" + l.Addr().String() + "/v1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.chat)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": ModelID, "object": "model"}}})
	})
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go s.srv.Serve(l)
	return s, nil
}

// Close останавливает заглушку.
func (s *Server) Close() error { return s.srv.Close() }

// Requests возвращает запросы, которые получила заглушка.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

var callPattern = regexp.MustCompile(`\[stub:call name=([A-Za-z0-9_.-]+) args=(\{.*?\})\]`)

type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"function"`
	} `json:"tools"`
}

// text достаёт текст из поля content: строка или массив частей.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rec := Request{Model: req.Model, Stream: req.Stream, APIKey: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")}
	for _, t := range req.Tools {
		rec.Tools = append(rec.Tools, t.Function.Name)
		rec.ToolInfo = append(rec.ToolInfo, ToolDef{Name: t.Function.Name, Description: t.Function.Description})
	}
	if n := len(req.Messages); n > 0 {
		last := req.Messages[n-1]
		rec.LastRole = last.Role
		rec.LastText = text(last.Content)
	}

	reply := Reply
	slow := false
	if rec.LastRole == "tool" {
		reply = AfterTool
	} else if rec.LastRole == "user" {
		for _, m := range callPattern.FindAllStringSubmatch(rec.LastText, -1) {
			rec.ToolCalls = append(rec.ToolCalls, Call{Name: m[1], Args: m[2]})
		}
		if strings.Contains(rec.LastText, SlowMarker) {
			slow = true
			reply = SlowReply
		}
	}
	s.mu.Lock()
	s.requests = append(s.requests, rec)
	s.mu.Unlock()

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}

	if !req.Stream {
		msg := map[string]any{"role": "assistant", "content": reply}
		finish := "stop"
		if len(rec.ToolCalls) > 0 {
			msg["content"] = nil
			msg["tool_calls"] = toolCalls(rec.ToolCalls, false)
			finish = "tool_calls"
		}
		writeJSON(w, map[string]any{
			"id": id, "object": "chat.completion", "created": created, "model": ModelID,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   usage,
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": ModelID,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}
	finish := "stop"
	if len(rec.ToolCalls) > 0 {
		send(chunk(map[string]any{"role": "assistant", "tool_calls": toolCalls(rec.ToolCalls, true)}, nil))
		finish = "tool_calls"
	} else {
		// Ответ идёт по словам, как у настоящей модели.
		send(chunk(map[string]any{"role": "assistant", "content": ""}, nil))
		words := strings.SplitAfter(reply, " ")
		for _, word := range words {
			if slow {
				select {
				case <-time.After(SlowDelay):
				case <-r.Context().Done():
					return // OpenCode остановил ответ
				}
			}
			send(chunk(map[string]any{"content": word}, nil))
		}
	}
	end := chunk(map[string]any{}, finish)
	end["usage"] = usage
	send(end)
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func toolCalls(calls []Call, stream bool) []any {
	out := make([]any, len(calls))
	for i, c := range calls {
		tc := map[string]any{
			"id":       fmt.Sprintf("call_%d", i+1),
			"type":     "function",
			"function": map[string]any{"name": c.Name, "arguments": c.Args},
		}
		if stream {
			tc["index"] = i
		}
		out[i] = tc
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
