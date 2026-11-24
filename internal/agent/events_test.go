package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseEvent(t *testing.T) {
	cases := []struct {
		raw  string
		want Event
		ok   bool
	}{
		{`{"type":"session.text.delta","data":{"sessionID":"s","assistantMessageID":"m","delta":"При"}}`,
			Event{Type: EventTextDelta, SessionID: "s", MessageID: "m", Delta: "При"}, true},
		{`{"type":"session.text.ended","data":{"sessionID":"s","assistantMessageID":"m","text":"Привет"}}`,
			Event{Type: EventTextEnded, SessionID: "s", MessageID: "m", Text: "Привет"}, true},
		{`{"type":"session.execution.succeeded","data":{"sessionID":"s"}}`,
			Event{Type: "session.execution.succeeded", SessionID: "s", Outcome: "succeeded"}, true},
		{`{"type":"session.execution.failed","data":{"sessionID":"s","error":{"message":"нет модели"}}}`,
			Event{Type: "session.execution.failed", SessionID: "s", Outcome: "failed", Error: "нет модели"}, true},
		{`{"type":"session.renamed","data":{"sessionID":"s","title":"Заголовок"}}`,
			Event{Type: EventRenamed, SessionID: "s", Title: "Заголовок"}, true},
		{`{"type":"provider.updated","data":{}}`, Event{}, false},
		{`{"type":"session.usage.updated","data":{"sessionID":"s"}}`, Event{}, false},
		{`не json`, Event{}, false},
	}
	for _, c := range cases {
		got, ok := ParseEvent([]byte(c.raw))
		if ok != c.ok || got != c.want {
			t.Errorf("ParseEvent(%s) = %+v, %v; ждали %+v, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
	started, _ := ParseEvent([]byte(`{"type":"session.execution.started","data":{"sessionID":"s"}}`))
	interrupted, _ := ParseEvent([]byte(`{"type":"session.execution.interrupted","data":{"sessionID":"s"}}`))
	if started.Finished() || !interrupted.Finished() || interrupted.Outcome != "interrupted" {
		t.Errorf("окончание: started=%+v interrupted=%+v", started, interrupted)
	}
}

func TestSubscribeReadsSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, p, _ := r.BasicAuth(); p != "секрет" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: {\"type\":\"server.connected\",\"data\":{}}\n\n")
		fmt.Fprint(w, "id: 1\ndata: {\"type\":\"session.text.delta\",\n")
		fmt.Fprint(w, "data: \"data\":{\"sessionID\":\"s\",\"delta\":\"Раз\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"session.execution.succeeded\",\"data\":{\"sessionID\":\"s\"}}\n\n")
	}))
	defer srv.Close()

	var got []string
	err := NewClient(srv.URL, "секрет").Subscribe(context.Background(), func(e Event) {
		got = append(got, e.Type+":"+e.Delta)
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "session.text.delta:Раз,session.execution.succeeded:" {
		t.Fatalf("события: %v", got)
	}
	if err := NewClient(srv.URL, "неверный").Subscribe(context.Background(), func(Event) {}); err == nil {
		t.Fatal("без пароля поток не открывается")
	}
}

func TestRunEventsReconnects(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintf(w, "data: {\"type\":\"session.text.delta\",\"data\":{\"sessionID\":\"s\",\"delta\":\"%d\"}}\n\n", calls)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seen := make(chan string, 10)
	go RunEvents(ctx, func() (*Client, error) { return NewClient(srv.URL, "x"), nil }, func(e Event) {
		seen <- e.Delta
	})
	// Сервер закрывает поток после одного события; подписка открывается снова.
	for _, want := range []string{"1", "2"} {
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("событие %q, ждали %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal("подписка не восстановилась")
		}
	}
}
