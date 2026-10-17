package main

import (
	"context"
	"example.com/els/api/internal/platform"
	"testing"
)

func TestApprovedRetrievalAndTutorPrivacy(t *testing.T) {
	t.Setenv("LEARNING_V3_TUTOR", "true")
	s, a, b := learningTestServer(t)
	doc, run, unit, u := seedReviewUnit(t, s)
	u.Kind = "text"
	u.Content = platform.Content{Text: "The book helped me."}
	u.Solution = platform.Solution{}
	_, err := s.db.Exec(`UPDATE learning_documents SET stage='library',published_run_id=$2 WHERE id=$1`, doc, run)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO learning_unit_publications(unit_id,revision,document_id,run_id,body,reviewer_id) VALUES($1,1,$2,$3,$4,'teacher')`, unit, doc, run, nullableJSON(u))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "demo"})
	found, err := s.retrieveApproved(ctx, "english", "book")
	if err != nil || len(found) != 1 || found[0].Page != 1 {
		t.Fatal(found, err)
	}
	if _, err = s.db.Exec(`UPDATE learning_unit_publications SET body='{}' WHERE unit_id=$1`, unit); err == nil {
		t.Fatal("publication body mutable")
	}
	s.db.Exec(`UPDATE learning_documents SET stage='B' WHERE id=$1`, doc)
	hidden, err := s.retrieveApproved(ctx, "english", "book")
	if err != nil || len(hidden) != 0 {
		t.Fatal("unreviewed resource retrieved", hidden, err)
	}
	s.db.Exec(`UPDATE learning_documents SET stage='library' WHERE id=$1`, doc)
	if validateTutorReply(tutorReply{Answer: "invented", Citations: []struct {
		ID    string `json:"id"`
		Quote string `json:"quote"`
	}{{"forged", "The book"}}}, found) {
		t.Fatal("forged citation accepted")
	}
	learningInsertQuestion(t, s, "english", "reading")
	attempt := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "english", "kind": "reading", "request_id": "tutor-source-start"}, 200)
	aid := rawString(attempt, "id")
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/tutor", map[string]any{"request_id": "tutor-before-submit", "question": "Explain", "mode": "full"}, 403)
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/submit", map[string]any{"answers": map[string]string{"1": "B"}}, 200)
	b.call("GET", "/api/v1/learning/attempts/"+aid+"/tutor", nil, 403)
	a.call("GET", "/api/v1/learning/attempts/"+aid+"/tutor", nil, 200)
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/tutor/state", map[string]string{"state": "completed"}, 200)
}
func TestHumanScoreIsAppendOnly(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_SKILLS", "true")
	s, a, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	q := learningInsertQuestion(t, s, "physics", "calculation")
	admin.call("POST", "/api/v1/admin/learning/questions/"+q+"/scoring-rules", map[string]any{"question_revision": 1, "source_revision": "Synthetic manually reviewed rubric", "rule": platform.ScoringRule{Parts: []platform.ScoringPart{{ID: "1", Mode: "manual"}}}}, 201)
	admin.call("POST", "/api/v1/admin/learning/questions/"+q+"/skill-links", map[string]any{"question_revision": 1, "links": []skillLink{{"physics-induction", "1", "primary"}}}, 201)
	first := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "physics", "kind": "calculation", "request_id": "manual-source-start"}, 200)
	id := rawString(first, "id")
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "My reasoning"}}, 200)
	input := map[string]any{"request_id": "manual-confirm-one", "parts": map[string]float64{"1": 1}, "note": "Test teacher checked the synthetic rubric and original response."}
	admin.call("POST", "/api/v1/admin/learning/attempts/"+id+"/confirm-score", input, 201)
	admin.call("POST", "/api/v1/admin/learning/attempts/"+id+"/confirm-score", input, 200)
	input["note"] = "different teacher note"
	admin.call("POST", "/api/v1/admin/learning/attempts/"+id+"/confirm-score", input, 409)
	for i := 0; i < 2; i++ {
		if err := s.processEvidenceOutbox(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM learning_score_evidence WHERE attempt_id=$1`, id).Scan(&n)
	if n != 2 {
		t.Fatal("scores overwritten or duplicated", n)
	}
	s.db.QueryRow(`SELECT independent_count FROM learning_user_skill_states WHERE user_id='demo' AND skill_id='physics-induction'`).Scan(&n)
	if n != 1 {
		t.Fatal("human evidence not consumed", n)
	}
}
