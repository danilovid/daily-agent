package agent

import (
	"strings"
	"testing"
)

func TestToolPolicy(t *testing.T) {
	read := []string{"get_issue", "list_issues", "searchCode", "getMergeRequest", "get_merge_request_diff", "list_pipeline_runs", "read-file", "query", "find_users", "describe_table"}
	write := []string{"create_issue", "delete_branch", "update_issue", "send_message", "getAndDelete", "get_issue_and_close", "merge_merge_request", "add_comment", "run_pipeline"}
	other := []string{"issues", "summarize", "ping", "issue_attachment_download", "workitems_for_users"}
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
	// Имена «сначала существительное», как у MCP-сервера YouTrack.
	youtrackRead := []string{"issue_details", "issues_search", "issue_comments", "issue_lookup", "issues_count", "issue_status",
		"issue_links", "issue_link_types", "issue_activities", "user_current", "service_info", "projects_list", "project_get",
		"workitems_list", "workitems_recent", "issues_starred_list", "article_get", "articles_search", "users_activity"}
	youtrackWrite := []string{"issue_create", "issue_update", "issue_assign", "issue_change_state", "issue_change_type",
		"issue_comment_create", "issue_comment_update", "issue_link_add", "issue_link_delete", "issue_attachment_delete",
		"issue_attachment_upload", "issue_star", "issue_unstar", "issues_star_batch", "issues_unstar_batch", "workitem_create",
		"workitem_create_idempotent", "workitem_delete", "workitem_update", "article_create", "article_update"}
	for _, name := range youtrackRead {
		if !ToolIsRead(name) || ToolIsWrite(name) {
			t.Errorf("%s — чтение", name)
		}
	}
	for _, name := range youtrackWrite {
		if ToolIsRead(name) || !ToolIsWrite(name) {
			t.Errorf("%s — запись", name)
		}
	}
	// Существительные, похожие на глаголы, не делают чтение записью.
	for _, name := range []string{"get_pipeline_run", "list_merge_request_comments", "get_commit_diff", "get_merge_request"} {
		if !ToolIsRead(name) {
			t.Errorf("%s — чтение", name)
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
