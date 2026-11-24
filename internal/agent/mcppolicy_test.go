package agent

import (
	"strings"
	"testing"
)

func TestToolPolicy(t *testing.T) {
	read := []string{"get_issue", "list_issues", "searchCode", "getMergeRequest", "get_merge_request_diff", "list_pipeline_runs", "read-file", "query", "find_users", "describe_table"}
	write := []string{"create_issue", "delete_branch", "update_issue", "send_message", "getAndDelete", "get_issue_and_close", "merge_merge_request", "add_comment", "run_pipeline"}
	other := []string{"issues", "summarize", "ping"}
	for _, name := range read {
		if !ToolIsRead(name) || ToolIsWrite(name) {
			t.Errorf("%s — чтение", name)
		}
	}
	for _, name := range write {
		if ToolIsRead(name) || !ToolIsWrite(name) {
			t.Errorf("%s — запись", name)
		}
	}
	for _, name := range other {
		if ToolIsRead(name) || ToolIsWrite(name) {
			t.Errorf("%s — ни чтение, ни запись по имени", name)
		}
	}
	p := ReadPatterns("tracker")
	if p[0] != "tracker_get*" || !strings.HasSuffix(p[len(p)-1], "*") {
		t.Fatalf("шаблоны: %v", p)
	}
	if ShortToolName("tracker", "tracker_get_issue") != "get_issue" {
		t.Fatal("короткое имя")
	}
}
