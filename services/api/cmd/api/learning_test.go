package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func learningTestServer(t *testing.T) (*server, *apiTester, *apiTester) {
	db := testDatabase(t)
	s := &server{db: db, origin: "http://localhost:3000", encryptionKey: bytes.Repeat([]byte{3}, 32)}
	if err := s.initLearning(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO users(id,display_name,username,password_hash,role) VALUES('teacher','老师','teacher',$1,'admin'),('other','另一账户','other',$1,'learner')`, security.HashPassword("learning-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET username='learning-student',password_hash=$1 WHERE id='demo'`, security.HashPassword("student-testing-password")); err != nil {
		t.Fatal(err)
	}
	client := func(uid string) *apiTester {
		token := security.Token(32)
		if _, err := db.Exec(`INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, security.Digest(token), uid); err != nil {
			t.Fatal(err)
		}
		return &apiTester{t: t, handler: s.routes(), token: token, csrf: security.CSRF(token)}
	}
	return s, client("demo"), client("other")
}
func learningInsertQuestion(t *testing.T, s *server, subject, kind string) string {
	t.Helper()
	id := security.Token(12)
	c := platform.Content{Text: "Choose.", Passage: "The book helped me.", Parts: []platform.Part{{ID: "1", Text: "Which?", Points: 2, Choices: []platform.Choice{{ID: "A", Text: "book"}, {ID: "B", Text: "pen"}}}}}
	sol := platform.Solution{Answers: map[string]string{"1": "A"}, Explanation: "The book is the subject."}
	raw, _ := json.Marshal(c)
	answer, _ := json.Marshal(sol)
	_, err := s.db.Exec(`INSERT INTO learning_chapters(id,subject_id,title,textbook,term) VALUES($1,$2,'Unit 1','必修一','高一上') ON CONFLICT DO NOTHING`, "ch-"+subject, subject)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO learning_questions(id,subject_id,chapter_id,kind,content,solution,provenance,source_key,status) VALUES($1,$2,$3,$4,$5,$6,'{}',$1,'published')`, id, subject, "ch-"+subject, kind, string(raw), string(answer))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestLearningDeadlinesIsolationAndWords(t *testing.T) {
	s, a, b := learningTestServer(t)
	learningInsertQuestion(t, s, "english", "reading")
	start := a.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "english", "kind": "reading", "request_id": "learning-start-one"}, 200)
	id := rawString(start, "id")
	raw, _ := json.Marshal(start)
	if strings.Contains(string(raw), "The book is the subject.") {
		t.Fatal("answer leaked")
	}
	b.call("GET", "/api/v1/learning/attempts/"+id, nil, 404)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/draft", map[string]any{"answers": map[string]string{"1": "B"}, "version": 2}, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/draft", map[string]any{"answers": map[string]string{"1": "A"}, "version": 1}, 409)
	s.db.Exec(`UPDATE learning_attempts SET deadline=now()-interval '1 second' WHERE id=$1`, id)
	result := a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	var out learningAttempt
	raw, _ = json.Marshal(result)
	json.Unmarshal(raw, &out)
	if out.Answers["1"] != "B" {
		t.Fatal("late answer replaced draft")
	}
	var f map[string]any
	json.Unmarshal(out.Feedback, &f)
	if f["score"] != float64(0) || f["timed_out"] != true {
		t.Fatal(f)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := a.request("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "A"}})
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Error(w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	choices := []platform.Choice{{ID: "A", Text: "adapt"}, {ID: "B", Text: "blue"}, {ID: "C", Text: "yesterday"}, {ID: "D", Text: "book"}}
	cs, _ := json.Marshal(choices)
	_, err := s.db.Exec(`INSERT INTO learning_word_sets(id,sense_id,direction,difficulty,prompt,choices,correct_id,approved,rationale) VALUES('set',1,'zh_en','normal','适应',$1,'A',true,'checked')`, string(cs))
	if err != nil {
		t.Fatal(err)
	}
	r := a.call("POST", "/api/v1/learning/words/next", map[string]any{"direction": "zh_en", "difficulty": "normal", "request_id": "learning-word-round"}, 201)
	rid := rawString(r, "id")
	if _, ok := r["correct_id"]; ok {
		t.Fatal("word answer leaked")
	}
	if strings.Contains(string(r["choices"]), "similar_group") {
		t.Fatal("pair labels leaked")
	}
	b.call("POST", "/api/v1/learning/words/"+rid+"/answer", map[string]string{"picked": "A"}, 404)
	s.db.Exec(`UPDATE learning_word_rounds SET deadline=now()-interval '1 second' WHERE id=$1`, rid)
	r = a.call("POST", "/api/v1/learning/words/"+rid+"/answer", map[string]string{"picked": "A"}, 200)
	if string(r["correct"]) != "false" {
		t.Fatal("late scored correct")
	}
	r = a.call("POST", "/api/v1/learning/words/"+rid+"/answer", map[string]string{"picked": "A"}, 200)
	if string(r["correct"]) != "false" {
		t.Fatal("retry changed score")
	}
}
func TestLearningLazyJobsChatAndPersonalSecrets(t *testing.T) {
	s, a, b := learningTestServer(t)
	learningInsertQuestion(t, s, "english", "reading")
	key, _ := security.Seal(s.encryptionKey, "secret-value", "provider:p")
	s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('p','Test','https://provider.test/v1',$1)`, key)
	var model int64
	if err := s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('p','test-model','Test','chat') RETURNING id`).Scan(&model); err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`INSERT INTO group_models(group_id,model_id) VALUES(1,$1)`, model)
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1`, fmt.Sprintf("[%d]", model))
	var calls atomic.Int64
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := json.Marshal(payload)
		if strings.Contains(string(raw), "another-question-sentinel") {
			t.Error("unrelated context")
		}
		messages := payload["messages"].([]any)
		prompt := messages[0].(map[string]any)["content"].(string)
		text := "这一步主语是The book。"
		if strings.Contains(prompt, "span_ids") {
			v := platform.Timeline{Version: 1, Text: "The book helped me.", Spans: []platform.Span{{ID: "subject", Start: 0, End: 8, Role: "主语", Tone: "yellow"}}, Steps: []platform.Step{{ID: "s1", Title: "找主语", Explanation: "The book是主语", SpanIDs: []string{"subject"}}}, Summary: "找到主干"}
			d, _ := json.Marshal(v)
			text = string(d)
		}
		out, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(out)), Header: http.Header{}}, nil
	})}
	start := a.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "english", "kind": "reading", "request_id": "learning-lazy-start"}, 200)
	id := rawString(start, "id")
	if calls.Load() != 0 {
		t.Fatal("AI before question reached")
	}

	a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "self_report"}, 403)
	var count int
	s.db.QueryRow(`SELECT count(*) FROM learning_jobs`).Scan(&count)
	if count != 0 || calls.Load() != 0 {
		t.Fatal("opening or early request queued AI")
	}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "A"}}, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "incorrect"}, 409)
	if calls.Load() != 0 {
		t.Fatal("correct answer spent model calls")
	}
	job := a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "self_report"}, 202)
	jid := rawString(job, "id")
	again := a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "self_report"}, 202)
	if jid != rawString(again, "id") {
		t.Fatal("duplicate job")
	}
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate call")
	}
	result := a.call("GET", "/api/v1/learning/jobs/"+jid, nil, 200)
	if string(result["status"]) != `"completed"` || string(result["result"]) == "null" {
		t.Fatal(result)
	}
	b.call("GET", "/api/v1/learning/jobs/"+jid, nil, 404)
	chat := map[string]any{"job_id": jid, "step_id": "s1", "content": "为什么？", "request_id": "learning-step-chat"}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/chat", chat, 200)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/chat", chat, 200)
	if calls.Load() != 2 {
		t.Fatal("chat replay spent another call")
	}
	a.call("POST", "/api/v1/learning/ai-settings", map[string]any{"base_url": "https://provider.test/v1", "model_id": "mine", "api_key": "my-private-secret", "enabled": true, "reviewer_models": []string{"review-a", "review-b"}}, 200)
	config := a.raw("GET", "/api/v1/learning/ai-settings", nil, 200)
	if strings.Contains(config.Body.String(), "my-private-secret") {
		t.Fatal("key disclosed")
	}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "demo"})
	m, err := s.personalModel(ctx)
	if err != nil || m.key != "my-private-secret" {
		t.Fatal("key not decrypted")
	}
	ctx2 := context.WithValue(context.Background(), identityKey{}, identity{ID: "other"})
	if _, err = s.personalModel(ctx2); err == nil {
		t.Fatal("cross-user key access")
	}
	s.db.Exec(`DELETE FROM group_models WHERE model_id=$1`, model)
	chat["request_id"] = "learning-revoked-chat"
	a.call("POST", "/api/v1/learning/attempts/"+id+"/chat", chat, 403)
}
func TestWritingCropAndCloudTraversal(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("word/document.xml")
	f.Write([]byte(`<w:document xmlns:w="urn:word"><w:body><w:p><w:r><w:t>第一段</w:t></w:r></w:p><w:p><w:r><w:t>第二段</w:t></w:r></w:p></w:body></w:document>`))
	z.Close()
	text, err := extractWriting("essay.docx", b.Bytes())
	if err != nil || text != "第一段\n第二段\n" {
		t.Fatal(text, err)
	}
	if _, err = extractWriting("essay.txt", []byte{0xff}); err == nil {
		t.Fatal("invalid UTF8")
	}
	if _, err = extractWriting("essay.doc", nil); err == nil {
		t.Fatal("old doc")
	}
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 100, 100)))
	crop, err := cropPNG(pngBytes.Bytes(), [4]float64{.25, .25, .75, .75})
	if err != nil {
		t.Fatal(err)
	}
	im, _ := png.Decode(bytes.NewReader(crop))
	if im.Bounds().Dx() != 50 {
		t.Fatal("crop bounds")
	}
	if _, err = cropPNG(pngBytes.Bytes(), [4]float64{-1, 0, 1, 1}); err == nil {
		t.Fatal("bad crop")
	}
	for _, path := range []string{"../secret", "a/../secret", "%2e%2e/secret", "https://other.test/x", "a?token=x", "a\\b"} {
		if _, err = cloudResource("https://cloud.test/dav/books", path); err == nil {
			t.Fatal(path)
		}
	}
}

func TestLearningPDFReviewPipeline(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("Poppler not installed")
	}
	s, a, _ := learningTestServer(t)
	aID := "teacher"
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: aID, Role: "admin"})
	key, _ := security.Seal(s.encryptionKey, "test-key", "provider:p")
	if _, err := s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('p','Test','https://provider.test/v1',$1)`, key); err != nil {
		t.Fatal(err)
	}
	var mid int64
	s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('p','vision-test','Vision','chat') RETURNING id`).Scan(&mid)
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1`, fmt.Sprintf("[%d]", mid))
	q, err := os.ReadFile("testdata/questions.pdf")
	if err != nil {
		t.Fatal(err)
	}
	ans, err := os.ReadFile("testdata/answers.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO learning_documents(id,owner_id,title,subject_id,question_pdf,answer_pdf) VALUES('doc',$1,'Original test fixture','english',$2,$3)`, aID, q, ans); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		messages := body["messages"].([]any)
		prompt := messages[0].(map[string]any)["content"].(string)
		text := `{"chapters":[{"key":"u1","title":"Unit 1","parent":"","question_pages":[1],"answer_pages":[1],"note":"test"}]}`
		if strings.Contains(prompt, "source_key") {
			parts := messages[1].(map[string]any)["content"].([]any)
			images := 0
			for _, p := range parts {
				if p.(map[string]any)["type"] == "image_url" {
					images++
				}
			}
			if images != 2 {
				t.Error("question and answer page images were not both provided")
			}
			text = `{"questions":[{"source_key":"1","kind":"reading","content":{"text":"Choose.","passage":"The book helped me.","parts":[{"id":"1","text":"What helped me?","choices":[{"id":"A","text":"The book"},{"id":"B","text":"The pen"}],"points":2}]},"solution":{"answers":{"1":"A"},"explanation":"The book is the subject.","rubric":"2 points"},"question_pages":[1],"answer_pages":[1],"answer_quote":"1. A. The book.","needs_review":false,"figures":[{"page":1,"bbox":[0.1,0.25,0.5,0.4],"alt":"Original book diagram"}]}]}`
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}}, nil
	})}
	if _, err = s.outlinePDF(ctx, "doc"); err != nil {
		t.Fatal(err)
	}
	chapter := chapterSelection{Key: "u1", Title: "Unit 1", QuestionPages: []int{1}, AnswerPages: []int{1}}
	if _, err = s.extractPDF(ctx, learningJobPayload{DocumentID: "doc", Chapters: []chapterSelection{chapter}}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("dynamic analysis generated at import")
	}
	var status string
	var imageCount int
	s.db.QueryRow(`SELECT status FROM learning_questions WHERE document_id='doc'`).Scan(&status)
	s.db.QueryRow(`SELECT count(*) FROM learning_assets`).Scan(&imageCount)
	if status != "draft" || imageCount != 1 {
		t.Fatal(status, imageCount)
	}
	if _, err = s.extractPDF(ctx, learningJobPayload{DocumentID: "doc", Chapters: []chapterSelection{chapter}}); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM learning_questions WHERE document_id='doc'`).Scan(&n)
	if n != 1 {
		t.Fatal("re-import duplicated question")
	}
	a.call("GET", "/api/v1/admin/learning/documents", nil, 403)
}

func TestLearningEssayPanelAndTrainingRules(t *testing.T) {
	s, a, _ := learningTestServer(t)
	qid := learningInsertQuestion(t, s, "chinese", "essay")
	content, _ := json.Marshal(platform.Content{Text: "写一篇关于学习的作文。", Parts: []platform.Part{{ID: "1", Text: "作文", Points: 60}}})
	solution, _ := json.Marshal(platform.Solution{Answers: map[string]string{"1": "合理立意"}, Explanation: "围绕成长展开。", Rubric: "内容、表达、发展等级，满分60。"})
	s.db.Exec(`UPDATE learning_questions SET content=$2,solution=$3 WHERE id=$1`, qid, string(content), string(solution))
	key, _ := security.Seal(s.encryptionKey, "key", "provider:essay")
	s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('essay','Essay','https://essay.test/v1',$1)`, key)
	ids := []int64{}
	for _, model := range []string{"review-a", "review-b", "lead"} {
		var id int64
		if err := s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('essay',$1,$1,'chat') RETURNING id`, model).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		s.db.Exec(`INSERT INTO group_models(group_id,model_id) VALUES(1,$1)`, id)
	}
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1,lead_model_id=$2 WHERE task='essay_reviewer'`, fmt.Sprintf("[%d,%d]", ids[0], ids[1]), ids[2])
	var reviewers atomic.Int64
	var leadCalls atomic.Int64
	var revokeLead atomic.Bool
	barrier := make(chan struct{})
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var result any
		if body.Model == "lead" {
			leadCalls.Add(1)
			if reviewers.Load() != 2 {
				t.Error("lead ran before independent reviews")
			}
			result = essayFinal{Score: 51, Reason: "综合原文内容与表达评价", Disagreement: "采纳双方都有原文依据的评价", Suggestions: []string{"增加具体例子"}}
		} else {
			if reviewers.Add(1) == 2 {
				close(barrier)
			}
			select {
			case <-barrier:
			case <-time.After(time.Second):
				t.Error("reviewers did not run concurrently")
			}
			if revokeLead.Load() {
				s.db.Exec(`DELETE FROM group_models WHERE model_id=$1`, ids[2])
			}
			result = essayReview{Score: 50, Reason: "内容完整，表达可提升", Evidence: []string{"学"}, Suggestions: []string{"补充论据"}}
		}
		text, _ := json.Marshal(result)
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(text)}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(out))}, nil
	})}
	start := a.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "chinese", "kind": "essay", "request_id": "essay-start-test"}, 200)
	id := rawString(start, "id")
	var deadline string
	json.Unmarshal(start["deadline"], &deadline)
	d, _ := time.Parse(time.RFC3339Nano, deadline)
	if time.Until(d) < 1790*time.Second || time.Until(d) > 1801*time.Second {
		t.Fatal("not a thirty-minute training deadline")
	}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": strings.Repeat("学", 850) + "!!!abc123"}}, 400)
	a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": strings.Repeat("学", 851)}, "model_selection": learningModelSelection{ReviewerIDs: []int64{ids[1], ids[0]}, LeadModelID: ids[2]}}, 200)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := a.call("GET", "/api/v1/learning/attempts/"+id, nil, 200)
	var f struct {
		Score   float64       `json:"score"`
		Reviews []essayReview `json:"independent_reviews"`
		Han     int           `json:"han_count"`
		Needs   bool          `json:"needs_ai"`
	}
	if err := json.Unmarshal(out["feedback"], &f); err != nil {
		t.Fatal(err)
	}
	if f.Score != 51 || len(f.Reviews) != 2 || f.Han != 851 || f.Needs || leadCalls.Load() != 1 {
		t.Fatalf("unexpected essay result: %+v lead=%d", f, leadCalls.Load())
	}
	revokeLead.Store(true)
	next := a.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "chinese", "kind": "essay", "request_id": "essay-revoke-lead"}, 200)
	nextID := rawString(next, "id")
	a.call("POST", "/api/v1/learning/attempts/"+nextID+"/submit", map[string]any{"answers": map[string]string{"1": strings.Repeat("学", 851)}, "model_selection": learningModelSelection{ReviewerIDs: []int64{ids[0], ids[1]}, LeadModelID: ids[2]}}, 200)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	s.db.QueryRow(`SELECT status FROM learning_jobs WHERE payload->>'attempt_id'=$1 AND kind='essay'`, nextID).Scan(&status)
	if status != "failed" || leadCalls.Load() != 1 {
		t.Fatal("revoked lead called after independent reviews", status)
	}
}

func TestLearningGroupChoicesAndRevokedQueue(t *testing.T) {
	s, a, b := learningTestServer(t)
	learningInsertQuestion(t, s, "english", "reading")
	key, _ := security.Seal(s.encryptionKey, "admin-secret", "provider:choices")
	if _, err := s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('choices','School','https://choices.test/v1',$1)`, key); err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, name := range []string{"first", "second", "hidden"} {
		var id int64
		if err := s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('choices',$1,$1,'chat') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	s.db.Exec(`INSERT INTO group_models(group_id,model_id) VALUES(1,$1),(1,$2)`, ids[0], ids[1])
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1`, fmt.Sprintf("[%d]", ids[2]))
	options := a.raw("GET", "/api/v1/learning/model-options", nil, 200)
	if strings.Contains(options.Body.String(), "admin-secret") || strings.Contains(options.Body.String(), "hidden") {
		t.Fatal("unauthorized model or credential exposed")
	}
	var o struct {
		Selection learningModelSelection `json:"selection"`
		Models    []aiModel              `json:"models"`
	}
	json.Unmarshal(options.Body.Bytes(), &o)
	if len(o.Models) != 2 || o.Selection.ModelID != ids[0] {
		t.Fatal(o)
	}
	choice := learningModelSelection{ModelID: ids[1], ReviewerIDs: []int64{ids[1], ids[0]}, LeadModelID: ids[0]}
	a.call("POST", "/api/v1/learning/model-preferences", choice, 200)
	a.call("POST", "/api/v1/learning/model-preferences", learningModelSelection{ModelID: ids[2]}, 403)
	a.call("POST", "/api/v1/learning/model-preferences", learningModelSelection{ReviewerIDs: []int64{ids[0], ids[0]}}, 403)
	mine := a.raw("GET", "/api/v1/learning/model-options", nil, 200)
	json.Unmarshal(mine.Body.Bytes(), &o)
	if o.Selection.ModelID != ids[1] || len(o.Selection.ReviewerIDs) != 2 {
		t.Fatal("selection not retained")
	}
	theirs := b.raw("GET", "/api/v1/learning/model-options", nil, 200)
	json.Unmarshal(theirs.Body.Bytes(), &o)
	if o.Selection.ModelID != ids[0] {
		t.Fatal("another user's preferences were copied")
	}
	var calls atomic.Int64
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "second" {
			t.Errorf("queued choice changed to %s", body.Model)
		}
		out := `{"choices":[{"message":{"content":"{\"version\":1,\"text\":\"The book helped me.\",\"spans\":[{\"id\":\"s\",\"start\":0,\"end\":8,\"role\":\"subject\",\"tone\":\"yellow\"}],\"steps\":[{\"id\":\"one\",\"title\":\"subject\",\"explanation\":\"book\",\"span_ids\":[\"s\"],\"asset_ids\":[]}],\"summary\":\"book\"}"},"finish_reason":"stop"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(out)), Header: http.Header{}}, nil
	})}
	start := a.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "english", "kind": "reading", "request_id": "choice-wrong-start"}, 200)
	id := rawString(start, "id")
	a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "self_report", "model_selection": choice}, 403)
	answer := a.call("POST", "/api/v1/learning/attempts/"+id+"/submit", map[string]any{"answers": map[string]string{"1": "B"}, "model_selection": learningModelSelection{ModelID: ids[2]}}, 200)
	if string(answer["explanation_needed"]) != "true" {
		t.Fatal("wrong answer did not enable explanation")
	}
	a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"model_selection": learningModelSelection{ModelID: ids[2]}}, 403)
	job := a.call("POST", "/api/v1/learning/attempts/"+id+"/explanation", map[string]any{"reason": "incorrect", "model_selection": choice}, 202)
	jid := rawString(job, "id")
	s.db.Exec(`DELETE FROM group_models WHERE model_id=$1`, ids[1])
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed := a.call("GET", "/api/v1/learning/jobs/"+jid, nil, 200)
	if string(failed["status"]) != `"failed"` || calls.Load() != 0 {
		t.Fatal("revoked queued model was called", failed)
	}
	s.db.Exec(`INSERT INTO group_models(group_id,model_id) VALUES(1,$1)`, ids[1])
	a.call("POST", "/api/v1/learning/jobs/"+jid+"/retry", map[string]any{}, 202)
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := a.call("GET", "/api/v1/learning/jobs/"+jid, nil, 200)
	if string(done["status"]) != `"completed"` || calls.Load() != 1 {
		t.Fatal("authorized selection did not run", done)
	}
}
func TestLearningSubjectiveExplanationWaitsForJudgment(t *testing.T) {
	now := time.Now()
	a := learningAttempt{Submitted: &now}
	a.Question.Content.Parts = []platform.Part{{ID: "1", Points: 4}}
	a.Feedback = json.RawMessage(`{"parts":[{"correct":false,"needs_ai":true}],"needs_ai":true}`)
	if learningExplanationNeeded(a) {
		t.Fatal("ungraded free text treated as wrong")
	}
	a.Feedback = json.RawMessage(`{"parts":[{"correct":false,"needs_ai":true}],"ai_grading":{"parts":[{"id":"1","score":4}]}}`)
	if learningExplanationNeeded(a) {
		t.Fatal("equivalent full-credit answer treated as wrong")
	}
	a.Feedback = json.RawMessage(`{"parts":[{"correct":false,"needs_ai":true}],"ai_grading":{"parts":[{"id":"1","score":2}]}}`)
	if !learningExplanationNeeded(a) {
		t.Fatal("graded lost points did not enable explanation")
	}
}
