// Package mcpstub — заглушка MCP-сервера для тестов: вымышленный трекер
// с инструментами на чтение и на запись. Работает через stdio (как локальный
// сервер) и через HTTP (как удалённый), по желанию — со входом через OAuth.
// Сеть — только 127.0.0.1.
package mcpstub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Tool — инструмент заглушки.
type Tool struct {
	Name        string
	Description string
	ReadOnly    bool
}

// Tools — инструменты вымышленного трекера.
var Tools = []Tool{
	{Name: "get_issue", Description: "Прочитать задачу трекера по номеру", ReadOnly: true},
	{Name: "list_issues", Description: "Список задач трекера", ReadOnly: true},
	{Name: "create_issue", Description: "Создать задачу в трекере", ReadOnly: false},
	// Имя «сначала существительное», как у некоторых серверов, например YouTrack.
	{Name: "issue_details", Description: "Подробности задачи трекера", ReadOnly: true},
}

// Reply — ответ инструмента.
const Reply = "Вымышленная задача TEST-1: проверить заглушку"

// OAuthToken — токен, который выдаёт вымышленный сервер авторизации.
const OAuthToken = "test-oauth-token"

// Handler отвечает на сообщения JSON-RPC и запоминает вызовы.
type Handler struct {
	mu    sync.Mutex
	calls []string
}

// Calls — имена вызванных инструментов.
func (h *Handler) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Handle отвечает на одно сообщение. Для уведомлений ответа нет: nil.
func (h *Handler) Handle(raw []byte) []byte {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return reply(nil, nil, &rpcError{Code: -32700, Message: "parse error"})
	}
	if len(req.ID) == 0 {
		return nil
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return reply(req.ID, map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "stub-tracker", "version": "0.1"},
		}, nil)
	case "tools/list":
		var list []map[string]any
		for _, t := range Tools {
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
				"annotations": map[string]any{"readOnlyHint": t.ReadOnly},
			})
		}
		return reply(req.ID, map[string]any{"tools": list}, nil)
	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		json.Unmarshal(req.Params, &p)
		h.mu.Lock()
		h.calls = append(h.calls, p.Name)
		h.mu.Unlock()
		return reply(req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": Reply}}}, nil)
	case "ping":
		return reply(req.ID, map[string]any{}, nil)
	default:
		return reply(req.ID, nil, &rpcError{Code: -32601, Message: "method not found"})
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func reply(id json.RawMessage, result any, err *rpcError) []byte {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		msg["error"] = err
	} else {
		msg["result"] = result
	}
	out, _ := json.Marshal(msg)
	return out
}

// ServeStdio — локальный сервер: сообщения по строкам на stdin и stdout.
// Вызовы инструментов и значения переменных из echo дописываются в журнал log.
func ServeStdio(r io.Reader, w io.Writer, log io.Writer, echo []string) error {
	h := &Handler{}
	for _, name := range echo {
		fmt.Fprintf(log, "env %s=%s\n", name, os.Getenv(name))
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		before := len(h.Calls())
		out := h.Handle([]byte(line))
		if calls := h.Calls(); len(calls) > before {
			fmt.Fprintf(log, "call %s\n", calls[len(calls)-1])
		}
		if out != nil {
			if _, err := w.Write(append(out, '\n')); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// Server — удалённый сервер на 127.0.0.1.
type Server struct {
	URL string // адрес MCP, например http://127.0.0.1:1234/mcp
	*Handler

	srv     *http.Server
	oauth   bool
	mu      sync.Mutex
	headers []http.Header
}

// Headers — заголовки запросов к MCP.
func (s *Server) Headers() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]http.Header(nil), s.headers...)
}

// Close останавливает сервер.
func (s *Server) Close() error { return s.srv.Close() }

// StartHTTP запускает удалённый сервер на свободном порту. См. StartHTTPAt.
func StartHTTP(oauth bool) (*Server, error) { return StartHTTPAt("127.0.0.1:0", oauth) }

// StartHTTPAt запускает удалённый сервер по адресу addr. oauth — требовать вход: без токена
// MCP отвечает 401, а рядом работает вымышленный сервер авторизации, который
// выдаёт OAuthToken без участия человека.
func StartHTTPAt(addr string, oauth bool) (*Server, error) {
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
	base := "http://" + l.Addr().String()
	s := &Server{URL: base + "/mcp", Handler: &Handler{}, oauth: oauth}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.headers = append(s.headers, r.Header.Clone())
		s.mu.Unlock()
		if oauth && r.Header.Get("Authorization") != "Bearer "+OAuthToken {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		out := s.Handle(raw)
		if out == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})
	if oauth {
		meta := func(w http.ResponseWriter, v any) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(v)
		}
		resource := func(w http.ResponseWriter, r *http.Request) {
			meta(w, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
		}
		mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", resource)
		mux.HandleFunc("/.well-known/oauth-protected-resource", resource)
		mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			meta(w, map[string]any{
				"issuer":                                base,
				"authorization_endpoint":                base + "/authorize",
				"token_endpoint":                        base + "/token",
				"registration_endpoint":                 base + "/register",
				"response_types_supported":              []string{"code"},
				"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		})
		mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["client_id"] = "test-client"
			body["client_id_issued_at"] = time.Now().Unix()
			w.WriteHeader(http.StatusCreated)
			meta(w, body)
		})
		// Вход без участия человека: сразу возвращает код в redirect_uri.
		mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			back, err := url.Parse(q.Get("redirect_uri"))
			if err != nil || back.Hostname() != "127.0.0.1" && back.Hostname() != "localhost" {
				http.Error(w, "redirect_uri только на этот компьютер", http.StatusBadRequest)
				return
			}
			v := back.Query()
			v.Set("code", "test-code")
			v.Set("state", q.Get("state"))
			back.RawQuery = v.Encode()
			http.Redirect(w, r, back.String(), http.StatusFound)
		})
		mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
			meta(w, map[string]any{"access_token": OAuthToken, "token_type": "Bearer", "expires_in": 3600})
		})
	}
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go s.srv.Serve(l)
	return s, nil
}
