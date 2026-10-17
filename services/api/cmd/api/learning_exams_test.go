package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestExamSnapshotTimingAndPrivacy(t *testing.T) {
	t.Setenv("LEARNING_V3_SCORING", "true")
	t.Setenv("LEARNING_V3_EXAMS", "true")
	s, a, b := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	q := learningInsertQuestion(t, s, "physics", "single_choice")
	approveFixtureEvidence(t, s, q, []skillLink{{"physics-induction", "1", "primary"}})
	count, seconds, points := 1, 1800, 2.0
	rule := examRule{Subject: "physics", Region: "广东", Category: "gaokao", Paper: "Synthetic platform paper", From: 2026, To: 2026, Verified: true, Seconds: &seconds, Total: &points, Sections: []examRuleSection{{Name: "Choice fixture", Kinds: []string{"single_choice"}, Count: &count, Points: &points}}, Sources: []string{"https://example.org/synthetic-rule"}, Notes: "Synthetic rule reviewed only for test; not official exam content."}
	saved := admin.call("POST", "/api/v1/admin/resources/exam-rules", rule, 201)
	rid := rawString(saved, "id")
	request := map[string]any{"request_id": "exam-create-one", "rule_id": rid, "year": 2026}
	paper := a.call("POST", "/api/v1/learning/exams", request, 200)
	var exam struct {
		ID string `json:"id"`
	}
	json.Unmarshal(paper["exam"], &exam)
	a.call("POST", "/api/v1/learning/exams", request, 200)
	var items []struct {
		Attempt learningAttempt `json:"attempt"`
	}
	json.Unmarshal(paper["items"], &items)
	if len(items) != 1 || len(items[0].Attempt.Question.Solution.Answers) != 0 {
		t.Fatal("paper leaked answer", items)
	}
	aid := items[0].Attempt.ID
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/draft", map[string]any{"version": 1, "answers": map[string]string{"1": "A"}}, 200)
	a.call("POST", "/api/v1/ai/training", map[string]any{}, 403)
	a.call("GET", "/api/v1/account/export", nil, 403)
	a.call("GET", "/api/v1/learning/skills", nil, 403)
	a.call("POST", "/api/v1/learning/attempts/"+aid+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 403)
	b.call("GET", "/api/v1/learning/exams/"+exam.ID, nil, 404)
	payload := map[string]any{"request_id": "exam-submit-one", "answers": map[string]map[string]string{aid: {"1": "A"}}}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRecorder()
			s.routes().ServeHTTP(r, a.request("POST", "/api/v1/learning/exams/"+exam.ID+"/submit", payload))
			if r.Code != 200 {
				t.Error(r.Code, r.Body.String())
			}
		}()
	}
	wg.Wait()
	payload["answers"] = map[string]map[string]string{aid: {"1": "B"}}
	a.call("POST", "/api/v1/learning/exams/"+exam.ID+"/submit", payload, 409)
	var n int
	var score float64
	err := s.db.QueryRow(`SELECT count(*),max(score) FROM learning_score_evidence WHERE attempt_id=$1`, aid).Scan(&n, &score)
	if err != nil || n != 1 || score != 2 {
		t.Fatal(n, score, err)
	}
	a.call("GET", "/api/v1/learning/exams/"+exam.ID, nil, 200)
	if _, err = s.db.Exec(`UPDATE learning_exam_sessions SET total_points=99 WHERE id=$1`, exam.ID); err == nil {
		t.Fatal("exam snapshot mutable")
	}
	// Insert a past-deadline fixture at creation; no trigger/constraint is disabled.
	_, err = s.db.Exec(`INSERT INTO learning_exam_sessions(id,user_id,rule_id,year,request_id,digest,rule_snapshot,deadline,total_points) VALUES('expired-exam','demo',$1,2026,'expired-request','fixture','{}',now()-interval '1 second',2)`, rid)
	if err != nil {
		t.Fatal(err)
	}
	var scoring string
	s.db.QueryRow(`SELECT id FROM learning_scoring_rule_revisions WHERE question_id=$1 ORDER BY revision DESC LIMIT 1`, q).Scan(&scoring)
	_, err = s.db.Exec(`INSERT INTO learning_attempts(id,user_id,question_id,question_revision,request_id,scoring_rule_revision_id,deadline,draft_answers,draft_version) VALUES('expired-item','demo',$1,1,'expired-item-request',$2,now()-interval '1 second','{"1":"B"}',1)`, q, scoring)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO learning_exam_items(exam_id,position,attempt_id,section_name,points) VALUES('expired-exam',1,'expired-item','fixture',2)`)
	if err != nil {
		t.Fatal(err)
	}
	a.call("POST", "/api/v1/learning/exams/expired-exam/submit", map[string]any{"request_id": "expired-submit-one", "answers": map[string]map[string]string{"expired-item": {"1": "A"}}}, 200)
	var original string
	s.db.QueryRow(`SELECT answers->>'1' FROM learning_attempts WHERE id='expired-item'`).Scan(&original)
	if original != "B" {
		t.Fatal("late answer replaced draft", original)
	}
	if err := s.expireLearningExam(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestExamSectionKeepsMaterialGroups(t *testing.T) {
	c := []examCandidate{{ID: "group", Content: platform.Content{Parts: []platform.Part{{ID: "1"}, {ID: "2"}}}, Points: 300}, {ID: "single", Content: platform.Content{Parts: []platform.Part{{ID: "1"}}}, Points: 200}}
	selected, ok := chooseExamSection(c, 3, 500)
	if !ok || len(selected) != 2 {
		t.Fatal(selected, ok)
	}
	if _, ok := chooseExamSection(c, 2, 200); ok {
		t.Fatal("invented matching points")
	}
}
