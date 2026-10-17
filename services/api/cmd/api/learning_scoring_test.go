package main

import (
	"encoding/json"
	"example.com/els/api/internal/platform"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestV3ScoringIdempotencyAndHistory(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	s, a, b := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	q := learningInsertQuestion(t, s, "math", "multiple_choice")
	var content platform.Content
	s.db.QueryRow(`SELECT content FROM learning_questions WHERE id=$1`, q).Scan(new(json.RawMessage))
	content = platform.Content{Text: "fixture", Parts: []platform.Part{{ID: "1", Points: 5, Choices: []platform.Choice{{ID: "A", Text: "a"}, {ID: "B", Text: "b"}, {ID: "C", Text: "c"}}}}}
	sol := platform.Solution{Answers: map[string]string{"1": "A,C"}, Explanation: "fixture explanation"}
	if _, err := s.db.Exec(`UPDATE learning_questions SET content=$2,solution=$3 WHERE id=$1`, q, nullableJSON(content), nullableJSON(sol)); err != nil {
		t.Fatal(err)
	}
	rule := platform.ScoringRule{Parts: []platform.ScoringPart{{ID: "1", Mode: "choice", Accepted: []string{"A,C"}, SubsetPoints: 2}}}
	admin.call("POST", "/api/v1/admin/learning/questions/"+q+"/scoring-rules", map[string]any{"question_revision": 2, "rule": rule, "source_revision": "synthetic fixture verified by test teacher"}, 201)
	input := map[string]any{"subject_id": "math", "kind": "multiple_choice", "request_id": "v3-create-test"}
	first := a.call("POST", "/api/v1/learning/attempts", input, 200)
	id := rawString(first, "id")
	input["kind"] = "single_choice"
	a.call("POST", "/api/v1/learning/attempts", input, 409)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]string{}, 403)
	b.call("GET", "/api/v1/learning/attempts/"+id, nil, 404)
	payload := map[string]any{"answers": map[string]string{"1": "A"}, "client_request_id": "v3-submit-test"}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			s.routes().ServeHTTP(rec, a.request("POST", "/api/v1/learning/attempts/"+id+"/submit", payload))
			if rec.Code != 200 {
				t.Error(rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	var n int
	var score float64
	if err := s.db.QueryRow(`SELECT count(*),max(score) FROM learning_score_evidence WHERE attempt_id=$1`, id).Scan(&n, &score); err != nil || n != 1 || score != 2 {
		t.Fatal(n, score, err)
	}
	payload["answers"] = map[string]string{"1": "C"}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", payload, 409)
	if _, err := s.db.Exec(`UPDATE learning_score_evidence SET score=5 WHERE attempt_id=$1`, id); err == nil {
		t.Fatal("evidence mutable")
	}
	if _, err := s.db.Exec(`UPDATE learning_attempts SET answers='{}' WHERE id=$1`, id); err == nil {
		t.Fatal("submitted answers mutable")
	}
	if _, err := s.db.Exec(`UPDATE learning_questions SET solution=jsonb_set(solution,'{answers,1}','"B"') WHERE id=$1`, q); err != nil {
		t.Fatal(err)
	}
	out := a.call("GET", "/api/v1/learning/attempts/"+id, nil, 200)
	var attempt learningAttempt
	raw, _ := json.Marshal(out)
	json.Unmarshal(raw, &attempt)
	if attempt.Question.Solution.Answers["1"] != "A,C" {
		t.Fatal("historical solution replaced")
	}
}
