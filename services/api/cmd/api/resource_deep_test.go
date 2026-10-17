package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDeepStrategiesAndIncompleteDrafts(t *testing.T) {
	for kind, want := range map[string]string{"textbook": "textbook", "grammar_practice": "topic", "official_paper": "exam", "mock_paper": "exam", "mixed": "mixed"} {
		if strategyForResource(kind) != want {
			t.Fatal(kind)
		}
	}
	u := deepUnit{Key: "1", Kind: "question", Title: "1", Subject: "english", Type: "grammar_practice", QuestionType: "unknown", Content: platform.Content{Text: "Choose.", Parts: []platform.Part{{ID: "1", Text: "Which?", Points: 0}}}, Pages: []int{1}, Evidence: []classificationEvidence{{1, "Choose."}}, Difficulty: "unknown", Confidence: .5}
	if err := validateUnitDraft(u, map[int]string{1: "Choose."}); err != nil {
		t.Fatal("missing answer/unknown type should remain reviewable", err)
	}
	if platform.ValidateQuestion(u.Subject, u.QuestionType, u.Content, u.Solution) == nil {
		t.Fatal("incomplete draft publishable")
	}
	u.Evidence[0].Quote = "fabricated"
	if validateUnitDraft(u, map[int]string{1: "Choose."}) == nil {
		t.Fatal("fabricated provenance")
	}
}
func TestDeepResumeRetainsUnitsAndAnswerUncertainty(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("Poppler required")
	}
	s, _, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	id, err := s.storeRawResource(ctx, "Grammar.pdf", pdf, nil, resourceSource{})
	if err != nil {
		t.Fatal(err)
	}
	admin.call("PATCH", "/api/v1/admin/resources/"+id+"/tags", resourceTags{Revision: 1, Type: "grammar_practice", Subjects: []string{"english"}, Alignment: "no", Confirm: true}, 200)
	cipher, _ := security.Seal(s.encryptionKey, "test-only", "provider:deep")
	s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('deep','Deep','https://models.test/v1',$1)`, cipher)
	var model int64
	if err = s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('deep','parser','Parser','chat') RETURNING id`).Scan(&model); err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1 WHERE task IN ('deep_parse','extract_questions','extract_answers','ocr')`, "["+jsonNumber(model)+"]")
	var questionCalls, answerCalls atomic.Int64
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		prompt := string(request.Messages[0].Content)
		result := ""
		if strings.Contains(prompt, "只提取当前页确实存在") {
			if answerCalls.Add(1) == 1 {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
			}
			result = `{"answers":[]}`
		} else {
			questionCalls.Add(1)
			result = `{"units":[{"source_key":"reading-1","unit_kind":"question","title":"Reading 1","subject_id":"english","resource_type":"grammar_practice","question_type":"reading","content":{"text":"Choose.","passage":"The book helped me.","parts":[{"id":"1","text":"What helped me?","points":2,"choices":[{"id":"A","text":"The book"},{"id":"B","text":"The pen"}]}]},"pages":[1],"evidence":[{"page":1,"quote":"The book helped me."}],"confidence":0.8,"difficulty":"unknown","needs_review":[]}]}`
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": result}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}}, nil
	})}
	queued := admin.call("POST", "/api/v1/admin/resources/"+id+"/deep-analyze", map[string]int{"revision": 2}, 202)
	job := rawString(queued, "job_id")
	if err = s.processLearningJob(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM learning_content_units WHERE document_id=$1`, id).Scan(&count)
	if count != 1 {
		t.Fatal("partial units lost", count)
	}
	s.db.Exec(`UPDATE learning_jobs SET retry_at=now() WHERE id=$1`, job)
	if err = s.processLearningJob(ctx); err != nil {
		t.Fatal(err)
	}
	var status, message string
	s.db.QueryRow(`SELECT status,error FROM learning_jobs WHERE id=$1`, job).Scan(&status, &message)
	if status != "completed" || questionCalls.Load() != 1 || answerCalls.Load() != 2 {
		t.Fatal(status, message, questionCalls.Load(), answerCalls.Load())
	}
	var body []byte
	s.db.QueryRow(`SELECT body FROM learning_content_units WHERE document_id=$1`, id).Scan(&body)
	var u deepUnit
	json.Unmarshal(body, &u)
	if len(u.Solution.Answers) != 0 {
		t.Fatal("answer invented")
	}
	s.db.QueryRow(`SELECT count(*) FROM learning_questions WHERE document_id=$1`, id).Scan(&count)
	if count != 0 {
		t.Fatal("AI output published before review")
	}
	admin.call("GET", "/api/v1/admin/resources/"+id+"/analysis", nil, 200)
}
