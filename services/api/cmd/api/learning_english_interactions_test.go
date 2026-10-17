package main

import (
	"strings"
	"testing"
)

func TestEnglishIndependentAttemptAndIsolation(t *testing.T) {
	s, a, b := learningTestServer(t)
	q := learningInsertQuestion(t, s, "english", "reading")
	out := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "english", "kind": "reading", "question_id": q, "request_id": "english-source-start"}, 200)
	aid := rawString(out, "id")
	input := map[string]string{"request_id": "syntax-create-1", "text": "The book helped me.", "source_attempt_id": aid}
	a.call("POST", "/api/v1/learning/english/syntax", input, 403)
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	created := a.call("POST", "/api/v1/learning/english/syntax", input, 200)
	id := rawString(created, "id")
	if string(created["analysis"]) != "null" {
		t.Fatal("analysis exposed before trying", created)
	}
	b.call("GET", "/api/v1/learning/english/syntax/"+id, nil, 404)
	a.call("POST", "/api/v1/learning/english/syntax/"+id+"/reveal", map[string]any{"request_id": "syntax-reveal-1", "model_id": 1}, 409)
	a.call("POST", "/api/v1/learning/english/syntax/"+id+"/attempt", map[string]any{"request_id": "syntax-attempt-1", "marks": []syntaxMark{{0, 999, "subject"}}}, 400)
	attempt := map[string]any{"request_id": "syntax-attempt-2", "marks": []syntaxMark{{0, 8, "subject"}}, "translation": "书帮助了我"}
	a.call("POST", "/api/v1/learning/english/syntax/"+id+"/attempt", attempt, 200)
	a.call("POST", "/api/v1/learning/english/syntax/"+id+"/attempt", attempt, 200)
	attempt["translation"] = "changed"
	a.call("POST", "/api/v1/learning/english/syntax/"+id+"/attempt", attempt, 409)
	if validSyntaxMarks("a😀b", []syntaxMark{{1, 3, "script"}}) {
		t.Fatal("unsafe role")
	}
	result := a.call("GET", "/api/v1/learning/english/syntax/"+id, nil, 200)
	if !strings.Contains(string(result["attempts"]), "subject") {
		t.Fatal("attempt lost")
	}
	a.call("POST", "/api/v1/learning/english/listening/nonexistent/reflection", map[string]string{"request_id": "listen-reflect-1", "reason": "heard_forgot"}, 403)
}
