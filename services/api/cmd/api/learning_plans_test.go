package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanningAndRecallIdempotency(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_PLANNING", "true")
	s, a, b := learningTestServer(t)
	q := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q, []skillLink{{"physics-induction", "1", "primary"}})
	request := map[string]any{"request_id": "v3-recommend-one", "subjects": []string{"physics"}, "limit": 3}
	out := a.call("POST", "/api/v1/learning/recommendations", request, 200)
	id := rawString(out, "id")
	a.call("GET", "/api/v1/learning/recommendations/"+id, nil, 200)
	b.call("GET", "/api/v1/learning/recommendations/"+id, nil, 404)
	request["limit"] = 4
	a.call("POST", "/api/v1/learning/recommendations", request, 409)
	attempt := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "physics", "kind": "single_choice", "question_id": q, "request_id": "v3-planning-start"}, 200)
	aid := rawString(attempt, "id")
	create := map[string]string{"attempt_id": aid, "front": "State the condition.", "back": "Synthetic self-authored answer."}
	a.call("POST", "/api/v1/learning/review-items", create, 403)
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	item := a.call("POST", "/api/v1/learning/review-items", create, 201)
	iid := rawString(item, "id")
	b.call("POST", "/api/v1/learning/review-items/"+iid+"/reveal", map[string]any{}, 404)
	a.call("POST", "/api/v1/learning/review-items/"+iid+"/reveal", map[string]any{}, 200)
	rate := map[string]any{"request_id": "v3-recall-rate", "version": 1, "rating": 3, "result": "correct", "used_hint": false}
	result := a.call("POST", "/api/v1/learning/review-items/"+iid+"/rate", rate, 200)
	a.call("POST", "/api/v1/learning/review-items/"+iid+"/rate", rate, 200)
	rate["rating"] = 4
	a.call("POST", "/api/v1/learning/review-items/"+iid+"/rate", rate, 409)
	undo := map[string]string{"request_id": "v3-recall-undo", "event_id": rawString(result, "event_id")}
	a.call("POST", "/api/v1/learning/review-items/"+iid+"/undo", undo, 200)
	a.call("POST", "/api/v1/learning/review-items/"+iid+"/undo", undo, 200)
	var restored bool
	if err := s.db.QueryRow(`SELECT fsrs_card IS NULL FROM learning_review_items WHERE id=$1`, iid).Scan(&restored); err != nil || !restored {
		t.Fatal(restored, err)
	}
	plan := a.call("POST", "/api/v1/learning/plans", map[string]any{"minutes": 20, "subjects": []string{"physics"}}, 200)
	var p map[string]json.RawMessage
	json.Unmarshal(plan["plan"], &p)
	var pid string
	json.Unmarshal(p["id"], &pid)
	b.call("PATCH", "/api/v1/learning/plans/"+pid, map[string]any{"request_id": "v3-plan-other", "version": 1, "skip_ids": []string{}}, 404)
	a.call("GET", "/api/v1/learning/report", nil, 200)
}

func TestRecommendationsUseReviewedPrerequisitesAndKeepReplay(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_SKILLS", "true")
	t.Setenv("LEARNING_V3_PLANNING", "true")
	s, a, b := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	if _, err := s.db.Exec(`INSERT INTO learning_terms(id,subject_id,kind,name) VALUES('physics-lenz','physics','knowledge','Synthetic Lenz prerequisite')`); err != nil {
		t.Fatal(err)
	}
	q := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q, []skillLink{{"physics-induction", "1", "primary"}})
	base := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, base, []skillLink{{"physics-lenz", "1", "primary"}})
	admin.call("POST", "/api/v1/admin/learning/skill-prerequisites", map[string]string{"skill_id": "physics-induction", "prerequisite_id": "physics-lenz"}, 200)
	admin.call("POST", "/api/v1/admin/learning/skill-prerequisites", map[string]string{"skill_id": "physics-lenz", "prerequisite_id": "physics-induction"}, 400)
	start := a.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "physics", "kind": "single_choice", "question_id": q, "request_id": "recommend-weak-start"}, 200)
	aid := rawString(start, "id")
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/submit", map[string]any{"answers": map[string]string{"1": "B"}}, 200)
	if err := s.processEvidenceOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/mistakes", map[string]any{"request_id": "recommend-confirm", "action": "correct", "type_id": "concept", "expected_version": 0}, 200)
	request := map[string]any{"request_id": "recommend-prerequisite", "subjects": []string{"physics"}, "limit": 2}
	out := a.call("POST", "/api/v1/learning/recommendations", request, 200)
	var selected []selectionCandidate
	if err := json.Unmarshal(out["selected"], &selected); err != nil || len(selected) != 2 {
		t.Fatal(string(out["selected"]), err)
	}
	if selected[0].ID != base || !strings.Contains(strings.Join(selected[0].Reasons, " "), "前置知识") || selected[0].RuleID == "" || selected[0].LinkID == "" {
		t.Fatal(selected)
	}
	if !strings.Contains(strings.Join(selected[1].Reasons, " "), "确认了相关错因") {
		t.Fatal(selected[1])
	}
	replay := a.call("POST", "/api/v1/learning/recommendations", request, 200)
	if string(replay["selected"]) != string(out["selected"]) {
		t.Fatal("recommendation replay changed")
	}
	request["request_id"] = "recommend-other-account"
	other := b.call("POST", "/api/v1/learning/recommendations", request, 200)
	if strings.Contains(string(other["selected"]), "前置知识") || strings.Contains(string(other["selected"]), "确认了相关错因") {
		t.Fatal("cross-account recommendation signals")
	}
}
