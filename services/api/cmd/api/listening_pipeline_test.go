package main

import (
	"bytes"
	"context"
	"encoding/json"
	"example.com/els/api/internal/security"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func learningTestModel(t *testing.T, s *server, name string) int64 {
	t.Helper()
	cipher, err := security.Seal(s.encryptionKey, "test-key", "provider:"+name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES($1,$1,'https://provider.test/v1',$2)`, name, cipher); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES($1,$1,$1,'chat') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO group_models(group_id,model_id) VALUES(1,$1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func testSpeakingLesson() listeningLesson {
	l := listeningLesson{Title: "校园采访", Format: "guangdong_b", Scenario: "你向同学了解图书馆活动", Transcript: strings.Repeat("The library activity starts at three on Friday. ", 6), Translation: "活动周五三点开始。"}
	for i := 0; i < 3; i++ {
		l.SpeakingTasks = append(l.SpeakingTasks, speakingTask{Kind: "ask", Cue: "询问活动何时开始", Reference: "When does the activity start?", Reply: "It starts at three on Friday.", Points: []string{"疑问词与助动词"}})
	}
	for i := 0; i < 5; i++ {
		l.SpeakingTasks = append(l.SpeakingTasks, speakingTask{Kind: "answer", Cue: "听问题后用英语回答", Question: "When does it start?", Reference: "At three on Friday.", Evidence: "starts at three on Friday", Points: []string{"三点，周五"}})
	}
	return l
}
func TestGuangdongTasksAndAnswerPrivacy(t *testing.T) {
	l := testSpeakingLesson()
	if err := l.validate(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(l)
	before, _ := json.Marshal(trainingPublic("lesson", "listening", raw, nil))
	for _, secret := range []string{l.Transcript, l.Translation, l.SpeakingTasks[0].Reference, l.SpeakingTasks[3].Question, "reference_tasks"} {
		if strings.Contains(string(before), secret) {
			t.Fatal("answer exposed before submission", secret)
		}
	}
	answers, _ := json.Marshal(listeningAnswers{RequestID: "speaking-answer-1", Responses: make([]string, 8)})
	after, _ := json.Marshal(trainingPublic("lesson", "listening", raw, answers))
	if !strings.Contains(string(after), "reference_tasks") {
		t.Fatal("reference missing after submission")
	}
	if audio, err := speakingSegment(l, "reply-0"); err != nil || audio != l.SpeakingTasks[0].Reply {
		t.Fatal(audio, err)
	}
	if _, err := speakingSegment(l, "reference-0"); err == nil {
		t.Fatal("audio endpoint exposed a reference answer")
	}
	l.SpeakingTasks = l.SpeakingTasks[:7]
	if l.validate() == nil {
		t.Fatal("incomplete 3+5 task set accepted")
	}
	l = testSpeakingLesson()
	l.SpeakingTasks[7].Evidence = "fabricated evidence"
	if l.validate() == nil {
		t.Fatal("fabricated evidence accepted")
	}
	for _, f := range []string{"guangdong_a", "guangdong_c"} {
		l = testSpeakingLesson()
		l.Format = f
		kind := "read"
		if f == "guangdong_c" {
			kind = "retell"
		}
		l.SpeakingTasks = []speakingTask{{Kind: kind, Cue: "完成专项", Reference: "Example response", Points: []string{"关键信息"}}}
		if err := l.validate(); err != nil {
			t.Fatal(f, err)
		}
	}
}
func TestSpeakingSubmissionIsolationAndReplay(t *testing.T) {
	s, a, b := learningTestServer(t)
	l := testSpeakingLesson()
	raw, _ := json.Marshal(l)
	_, err := s.db.Exec(`INSERT INTO training_sessions(id,user_id,kind,title,payload) VALUES('speaking','demo','listening','听说',$1)`, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	b.call("GET", "/api/v1/ai/training/speaking", nil, 404)
	a.call("POST", "/api/v1/ai/training/speaking/feedback", map[string]any{"model_id": 1, "request_id": "feedback-before-answer"}, 409)
	a.call("POST", "/api/v1/ai/training/speaking/answers", listeningAnswers{RequestID: "too-few-responses"}, 400)
	input := listeningAnswers{RequestID: "speaking-submit-1", Responses: []string{"When?", "Where?", "Who?", "At three.", "Friday.", "Library.", "Classmates.", "Reading."}, Notes: "Fri 3pm"}
	a.call("POST", "/api/v1/ai/training/speaking/answers", input, 200)
	a.call("POST", "/api/v1/ai/training/speaking/answers", input, 200)
	input.Responses[0] = "changed"
	a.call("POST", "/api/v1/ai/training/speaking/answers", input, 409)
	b.call("POST", "/api/v1/ai/training/speaking/answers", input, 404)
	data := a.call("GET", "/api/v1/ai/training/speaking", nil, 200)
	if len(data["reference_tasks"]) == 0 {
		t.Fatal("submitted references missing")
	}
}
func TestOCRIsIndependentAndRetainsPageProgress(t *testing.T) {
	s, _, _ := learningTestServer(t)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	q, _ := os.ReadFile("testdata/questions.pdf")
	a, _ := os.ReadFile("testdata/answers.pdf")
	if _, err := s.db.Exec(`INSERT INTO learning_documents(id,owner_id,title,subject_id,question_pdf,answer_pdf) VALUES('ocr','teacher','Scan','english',$1,$2)`, q, a); err != nil {
		t.Fatal(err)
	}
	if err := s.preparePDF(ctx, "ocr"); err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`UPDATE learning_pages SET text_source='pending',text_content='' WHERE document_id='ocr'`)
	// No OCR model: fail clearly rather than silently use the directory model.
	if _, err := s.recognizePDF(ctx, "ocr"); err == nil || !strings.Contains(err.Error(), "OCR") {
		t.Fatal(err)
	}
	model := learningTestModel(t, s, "ocr-model")
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=jsonb_build_array($1::bigint) WHERE task='ocr'`, model)
	calls := 0
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		text := `{"text":"Question 1. Choose the best answer.","uncertain":["核对图示"]}`
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	if _, err := s.recognizePDF(ctx, "ocr"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("OCR did not process both source PDFs", calls)
	}
	if _, err := s.recognizePDF(ctx, "ocr"); err != nil || calls != 2 {
		t.Fatal("completed OCR pages were regenerated", err, calls)
	}
}
