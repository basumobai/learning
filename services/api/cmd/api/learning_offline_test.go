package main

import (
	"strings"
	"testing"
)

func TestDraftCompareAndSwapAndSafeLearningExport(t *testing.T) {
	s, a, b := learningTestServer(t)
	learningInsertQuestion(t, s, "english", "reading")
	out := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "english", "kind": "reading", "request_id": "cas-draft-create"}, 200)
	id := rawString(out, "id")
	first := map[string]any{"version": 1, "base_version": 0, "answers": map[string]string{"1": "B"}}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/draft", first, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/draft", first, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/draft", map[string]any{"version": 2, "base_version": 0, "answers": map[string]string{"1": "A"}}, 409)
	exported := a.raw("GET", "/api/v1/account/export", nil, 200)
	if strings.Contains(exported.Body.String(), "The book is the subject.") {
		t.Fatal("unsubmitted solution leaked in export")
	}
	other := b.raw("GET", "/api/v1/account/export", nil, 200)
	if strings.Contains(other.Body.String(), id) {
		t.Fatal("cross-user export")
	}
	a.call("GET", "/api/v1/admin/learning/operations", nil, 403)
}
