package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMistakeCorrectionCompensatesWithoutDeletingFacts(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_SKILLS", "true")
	s, a, b := learningTestServer(t)
	q := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q, []skillLink{{"physics-induction", "1", "primary"}})
	out := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "physics", "kind": "single_choice", "request_id": "v3-mistake-start"}, 200)
	id := rawString(out, "id")
	a.call("POST", "/api/v1/learning/attempts/"+id+"/diagnosis", map[string]any{}, 503)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "B"}}, 200)
	if err := s.processEvidenceOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	correction := map[string]any{"request_id": "v3-correction-one", "expected_version": 0, "action": "correct", "type_id": "input_error", "note": "输入失误"}
	b.call("POST", "/api/v1/learning/attempts/"+id+"/mistakes", correction, 404)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/mistakes", correction, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/mistakes", correction, 200)
	correction["note"] = "changed"
	a.call("POST", "/api/v1/learning/attempts/"+id+"/mistakes", correction, 409)
	var count int
	s.db.QueryRow(`SELECT independent_count FROM learning_user_skill_states WHERE user_id='demo' AND skill_id='physics-induction'`).Scan(&count)
	if count != 0 {
		t.Fatal("input mistake still affects skill", count)
	}
	s.db.QueryRow(`SELECT count(*) FROM learning_skill_evidence WHERE user_id='demo'`).Scan(&count)
	if count != 1 {
		t.Fatal("fact deleted", count)
	}
	s.db.QueryRow(`SELECT count(*) FROM learning_mistake_events WHERE attempt_id=$1`, id).Scan(&count)
	if count != 1 {
		t.Fatal("correction replay duplicated", count)
	}
	var attempt learningAttempt
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "demo"})
	attempt, _ = s.loadAttempt(ctx, id)
	if validateCandidate(attempt, mistakeCandidate{Part: "1", Type: "concept", QuestionQuote: "fabricated", AnswerQuote: "B", Uncertainty: "candidate only"}) {
		t.Fatal("fabricated evidence accepted")
	}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/transfer", map[string]string{"request_id": "v3-no-transfer"}, 404)
	q2 := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q2, []skillLink{{"physics-induction", "1", "primary"}})
	next := a.call("POST", "/api/v1/learning/attempts/"+id+"/transfer", map[string]string{"request_id": "v3-new-transfer"}, 200)
	var fresh learningAttempt
	raw, _ := json.Marshal(next)
	json.Unmarshal(raw, &fresh)
	if fresh.Question.ID == q || len(fresh.Question.Solution.Answers) > 0 {
		t.Fatal("transfer reused or exposed answer")
	}
	a.call("POST", "/api/v1/learning/attempts/"+fresh.ID+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	if err := s.processEvidenceOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`SELECT transfer_correct FROM learning_user_skill_states WHERE user_id='demo' AND skill_id='physics-induction'`).Scan(&count)
	if count != 1 {
		t.Fatal("new material not recorded", count)
	}
}
