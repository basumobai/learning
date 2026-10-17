package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"os"
	"strings"
	"testing"
)

func seedReviewUnit(t *testing.T, s *server) (string, string, string, deepUnit) {
	t.Helper()
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	doc, err := s.storeRawResource(ctx, "Review.pdf", pdf, nil, resourceSource{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`UPDATE learning_documents SET stage='C',classification_status='confirmed',subject_id='english',resource_type='grammar_practice' WHERE id=$1`, doc)
	if err != nil {
		t.Fatal(err)
	}
	run := "review-run"
	_, err = s.db.Exec(`INSERT INTO learning_analysis_runs(id,document_id,resource_revision,strategy,config_hash,status,total_pages,processed_pages) VALUES($1,$2,1,'topic','fixture','ready',1,1)`, run, doc)
	if err != nil {
		t.Fatal(err)
	}
	page := samplePage{1, "Unit 1 - Reading\nThe book helped me.\n1. What helped me?\nA. The book B. The pen", "native"}
	s.db.Exec(`INSERT INTO learning_analysis_steps(run_id,step_key,result) VALUES($1,'page:question:1',$2)`, run, nullableJSON(page))
	u := deepUnit{Key: "reading-1", Kind: "question", Title: "Reading 1", Subject: "english", Type: "grammar_practice", QuestionType: "reading", Content: platform.Content{Text: "Choose.", Passage: "The book helped me.", Parts: []platform.Part{{ID: "1", Text: "What helped me?", Points: 2, Choices: []platform.Choice{{ID: "A", Text: "The book"}, {ID: "B", Text: "The pen"}}}}}, Solution: platform.Solution{}, Pages: []int{1}, Evidence: []classificationEvidence{{1, "The book helped me."}}, Knowledge: []string{"en-reading"}, Confidence: .8, Authenticity: "unverified", Difficulty: "unknown"}
	unit := "review-unit"
	_, err = s.db.Exec(`INSERT INTO learning_content_units(id,run_id,document_id,unit_key,body) VALUES($1,$2,$3,'stable-original-unit',$4)`, unit, run, doc, nullableJSON(u))
	if err != nil {
		t.Fatal(err)
	}
	return doc, run, unit, u
}
func jobStatus(t *testing.T, s *server, id string) (string, string) {
	t.Helper()
	var status, message string
	if err := s.db.QueryRow(`SELECT status,error FROM learning_jobs WHERE id=$1`, id).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	return status, message
}
func TestReviewedIndependentQuestionAndAttemptSnapshots(t *testing.T) {
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	doc, run, unit, u := seedReviewUnit(t, s)
	path := "/api/v1/admin/resources/" + doc
	payload := publishRequest{Revision: 1, Run: run, Units: []reviewSelection{{unit, 1}}, Note: "核对来源页、题型与答案"}
	bad := admin.call("POST", path+"/approve", payload, 202)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state, _ := jobStatus(t, s, rawString(bad, "job_id")); state != "failed" {
		t.Fatal("missing answer published", state)
	}
	u.Solution = platform.Solution{Answers: map[string]string{"1": "A"}, Explanation: "The book is the subject."}
	u.AnswerOrigin = "human"
	u.HumanAnswerNote = "管理员根据原题逐项核验了主语和答案选项，确认A。"
	admin.call("PATCH", path+"/units/"+unit, map[string]any{"revision": 1, "resource_revision": 1, "body": u}, 200)
	payload.Units[0].Revision = 2
	good := admin.call("POST", path+"/approve", payload, 202)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state, msg := jobStatus(t, s, rawString(good, "job_id")); state != "completed" {
		t.Fatal(state, msg)
	}
	original := student.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "english", "kind": "reading", "request_id": "independent-original"}, 200)
	attempt := rawString(original, "id")
	var q learningQuestion
	json.Unmarshal(original["question"], &q)
	if q.Chapter != "" || len(q.Solution.Answers) != 0 {
		t.Fatal("independent topic forced into chapter or answer exposed", q)
	}
	u.Content.Text = "Choose the revised answer."
	u.Solution.Answers["1"] = "B"
	u.Solution.Explanation = "Revised reference explanation."
	admin.call("PATCH", path+"/units/"+unit, map[string]any{"revision": 2, "resource_revision": 1, "body": u}, 200)
	payload.Units[0].Revision = 3
	republish := admin.call("POST", path+"/approve", payload, 202)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state, msg := jobStatus(t, s, rawString(republish, "job_id")); state != "completed" {
		t.Fatal(state, msg)
	}
	old := student.call("GET", "/api/v1/learning/attempts/"+attempt, nil, 200)
	json.Unmarshal(old["question"], &q)
	if q.Content.Text != "Choose." {
		t.Fatal("ongoing attempt changed", q.Content)
	}
	submitted := student.call("POST", "/api/v1/learning/attempts/"+attempt+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	if !strings.Contains(string(submitted["feedback"]), `"score":2`) {
		t.Fatal("old attempt graded against new answer", submitted)
	}
	fresh := student.call("POST", "/api/v1/learning/attempts", map[string]string{"subject_id": "english", "kind": "reading", "request_id": "independent-revised"}, 200)
	json.Unmarshal(fresh["question"], &q)
	if q.Content.Text != u.Content.Text {
		t.Fatal("new attempt did not use revised question")
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM learning_questions WHERE document_id=$1`, doc).Scan(&count)
	if count != 1 {
		t.Fatal("republish duplicated questions", count)
	}
	library := student.raw("GET", "/api/v1/resources/library/"+doc, nil, 200)
	if strings.Contains(library.Body.String(), "Revised reference explanation.") || strings.Contains(library.Body.String(), "human_answer_note") {
		t.Fatal("library bypassed answer privacy")
	}
	if _, err := s.db.Exec(`UPDATE learning_question_revisions SET content='{}'`); err == nil {
		t.Fatal("question snapshots mutable")
	}
}
func TestExamVerificationAndConfigurationRevisions(t *testing.T) {
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	doc, run, unit, u := seedReviewUnit(t, s)
	u.Type = "official_paper"
	u.Solution = platform.Solution{Answers: map[string]string{"1": "A"}, Explanation: "Original reference."}
	u.AnswerOrigin = "human"
	u.HumanAnswerNote = "管理员核验了试题和原参考答案，题目答案确定为A。"
	s.db.Exec(`UPDATE learning_content_units SET body=$2 WHERE id=$1`, unit, nullableJSON(u))
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	tx, _ := s.db.BeginTx(ctx, nil)
	if err := validatePublishUnit(ctx, tx, u, run); err == nil {
		t.Fatal("filename alone confirmed official paper")
	}
	tx.Rollback()
	rule := examRule{Subject: "english", Region: "广东", Category: "gaokao", Paper: "Test fixture only", From: 2025, To: 2025, Sections: []examRuleSection{{Name: "Reading", Kinds: []string{"reading"}}}, Sources: []string{"https://example.org/official-fixture"}, Notes: "仅为合成测试核验记录，不代表真实高考规则", Verified: true}
	out := admin.call("POST", "/api/v1/admin/resources/exam-rules", rule, 201)
	var saved examRule
	b, _ := json.Marshal(out)
	json.Unmarshal(b, &saved)
	u.RuleID = saved.ID
	year := 2025
	u.ExamYear = &year
	u.ExamRegion = "广东"
	u.ExamName = "Fixture exam"
	u.Authenticity = "verified"
	u.VerificationSource = "https://example.org/official-fixture"
	tx, _ = s.db.BeginTx(ctx, nil)
	if err := validatePublishUnit(ctx, tx, u, run); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	year = 2026
	tx, _ = s.db.BeginTx(ctx, nil)
	if err := validatePublishUnit(ctx, tx, u, run); err == nil {
		t.Fatal("wrong year rule accepted")
	}
	tx.Rollback()
	student.call("GET", "/api/v1/learning/catalog?year=2027", nil, 200)
	input := map[string]any{"subject_id": "physics", "kind": "knowledge", "name": "测试电磁感应知识点"}
	term := admin.call("POST", "/api/v1/admin/resources/terms", input, 200)
	id := rawString(term, "id")
	input["id"] = id
	input["revision"] = 1
	input["name"] = "测试电磁感应修订"
	admin.call("POST", "/api/v1/admin/resources/terms", input, 200)
	admin.call("POST", "/api/v1/admin/resources/terms", input, 409)
	input["revision"] = 2
	input["parent_id"] = id
	admin.call("POST", "/api/v1/admin/resources/terms", input, 400)
	student.call("GET", "/api/v1/admin/resources/curriculum", nil, 403)
	admin.call("GET", "/api/v1/admin/resources/"+doc+"/analysis", nil, 200)
}
