package main

import (
	"context"
	"example.com/els/api/internal/platform"
	"fmt"
	"testing"
)

func approveFixtureEvidence(t *testing.T, s *server, q string, links []skillLink) {
	t.Helper()
	admin := curriculumAdmin(t, s)
	var revision int
	s.db.QueryRow(`SELECT revision FROM learning_questions WHERE id=$1`, q).Scan(&revision)
	admin.call("POST", "/api/v1/admin/learning/questions/"+q+"/scoring-rules", map[string]any{"question_revision": revision, "source_revision": "synthetic teacher review", "rule": platform.ScoringRule{Parts: []platform.ScoringPart{{ID: "1", Mode: "choice", Accepted: []string{"A"}}}}}, 201)
	admin.call("POST", "/api/v1/admin/learning/questions/"+q+"/skill-links", map[string]any{"question_revision": revision, "links": links}, 201)
}
func TestV3SkillEvidenceIsSeparateAndReplayable(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_SKILLS", "true")
	s, a, b := learningTestServer(t)
	q := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q, []skillLink{{"physics-induction", "1", "primary"}})
	for i := 0; i < 3; i++ {
		out := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "physics", "kind": "single_choice", "request_id": fmt.Sprintf("v3-skill-create-%d", i)}, 200)
		id := rawString(out, "id")
		if i == 2 {
			a.call("POST", "/api/v1/learning/attempts/"+id+"/hint", map[string]string{"request_id": "v3-skill-hint"}, 200)
			a.call("POST", "/api/v1/learning/attempts/"+id+"/hint", map[string]string{"request_id": "v3-skill-hint"}, 200)
		}
		a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
		if err := s.processEvidenceOutbox(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.processEvidenceOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	var independent, repeat, assisted int
	var state string
	err := s.db.QueryRow(`SELECT independent_count,repeat_count,assisted_count,state FROM learning_user_skill_states WHERE user_id='demo' AND skill_id='physics-induction'`).Scan(&independent, &repeat, &assisted, &state)
	if err != nil || independent != 1 || repeat != 1 || assisted != 1 || state != "limited" {
		t.Fatal(independent, repeat, assisted, state, err)
	}
	own := a.call("GET", "/api/v1/learning/skills/physics-induction/evidence", nil, 200)
	if string(own["evidence"]) == "[]" {
		t.Fatal("missing evidence")
	}
	other := b.call("GET", "/api/v1/learning/skills/physics-induction/evidence", nil, 200)
	if string(other["evidence"]) != "[]" {
		t.Fatal("cross user evidence")
	}
	if _, err = s.db.Exec(`UPDATE learning_skill_evidence SET correct=false`); err == nil {
		t.Fatal("skill fact mutable")
	}
}
