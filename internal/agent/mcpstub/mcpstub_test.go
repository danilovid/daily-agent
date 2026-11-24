package mcpstub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStdioAndHTTP(t *testing.T) {
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_issue"}}`,
	}, "\n")
	var out, log bytes.Buffer
	if err := ServeStdio(strings.NewReader(in), &out, &log, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "create_issue") || !strings.Contains(log.String(), "call get_issue") {
		t.Fatalf("stdio: %v / %s", lines, log.String())
	}

	if _, err := StartHTTPAt("0.0.0.0:0", false); err == nil {
		t.Fatal("заглушка не должна слушать все адреса")
	}
	srv, err := StartHTTP(true)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	post := func(token string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if post("") != http.StatusUnauthorized || post(OAuthToken) != http.StatusOK {
		t.Fatal("вход по токену")
	}
	resp, _ := http.Get(strings.TrimSuffix(srv.URL, "/mcp") + "/.well-known/oauth-authorization-server")
	var meta map[string]any
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta["authorization_endpoint"] == nil {
		t.Fatalf("метаданные сервера авторизации: %v", meta)
	}
}
