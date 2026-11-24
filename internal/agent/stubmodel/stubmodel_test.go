package stubmodel

import (
	"net"
	"net/http"
	"strconv"
	"testing"
)

func TestStartAt(t *testing.T) {
	if _, err := StartAt("0.0.0.0:0"); err == nil {
		t.Fatal("заглушка не должна слушать все адреса")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv, err := StartAt("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.URL != "http://127.0.0.1:"+strconv.Itoa(port)+"/v1" {
		t.Fatalf("адрес: %s", srv.URL)
	}
	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ответ: %d", resp.StatusCode)
	}
}

func TestParseCalls(t *testing.T) {
	text := `Сделай [stub:call name=daily_get_day args={}] и [stub:call name=daily_set_day_plan args={"items":[{"task_id":"a","estimate_min":30}],"summary":"x]y"}] [stub:call name=bad args={oops}]`
	calls := parseCalls(text)
	if len(calls) != 2 || calls[0].Name != "daily_get_day" || calls[0].Args != "{}" ||
		calls[1].Args != `{"items":[{"task_id":"a","estimate_min":30}],"summary":"x]y"}` {
		t.Fatalf("вызовы: %+v", calls)
	}
}
