package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

func TestClassificationEvidenceAndUncertainty(t *testing.T) {
	pages := map[int]string{1: "北师大版2019 英语 选择性必修第一册\nUnit 1 Relationships"}
	valid := documentRecognition{Subject: "english", Edition: platform.EnglishEdition, Volume: "选择性必修第一册", SourceKind: "exercise", Confidence: "high", Evidence: []classificationEvidence{{1, "Unit 1 Relationships"}}}
	if err := valid.validate(pages); err != nil {
		t.Fatal(err)
	}
	for _, e := range []classificationEvidence{{2, "Unit 1 Relationships"}, {1, "Unit 2 Success"}, {1, ""}} {
		bad := valid
		bad.Evidence = []classificationEvidence{e}
		if bad.validate(pages) == nil {
			t.Fatal("fabricated evidence accepted", e)
		}
	}
	unknown := documentRecognition{Subject: "unknown", Confidence: "low", SourceKind: "unknown"}
	if unknown.validate(pages) != nil {
		t.Fatal("uncertainty cannot be represented")
	}
	for _, v := range []string{"人教版2019", "PEP 2019", "外研版", "北师大2003"} {
		if !otherEnglishEdition(v) {
			t.Fatal("English edition conflict missed", v)
		}
	}
}

func curriculumAdmin(t *testing.T, s *server) *apiTester {
	t.Helper()
	token := security.Token(32)
	if _, err := s.db.Exec(`INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,'teacher',now()+interval '1 day')`, security.Digest(token)); err != nil {
		t.Fatal(err)
	}
	return &apiTester{t: t, handler: s.routes(), token: token, csrf: security.CSRF(token)}
}

func TestPDFClassificationRequiresScopeConfirmation(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("Poppler not installed")
	}
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	q, _ := os.ReadFile("testdata/questions.pdf")
	a, _ := os.ReadFile("testdata/answers.pdf")
	dir := t.TempDir()
	t.Setenv("LEARNING_PDF_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "q.pdf"), q, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), a, 0600); err != nil {
		t.Fatal(err)
	}
	key, _ := security.Seal(s.encryptionKey, "disposable-model-key", "provider:curriculum")
	if _, err := s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('curriculum','Test','https://provider.test/v1',$1)`, key); err != nil {
		t.Fatal(err)
	}
	var model int64
	if err := s.db.QueryRow(`INSERT INTO ai_models(provider_id,model_id,name,kind) VALUES('curriculum','classifier','Classifier','chat') RETURNING id`).Scan(&model); err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1 WHERE task='classify_document'`, "["+jsonNumber(model)+"]")
	texts, err := pdfTexts(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	quote := strings.Fields(texts[0])
	if len(quote) < 4 {
		t.Fatal("test PDF lacks original text")
	}
	proposal := documentRecognition{Subject: "english", Edition: platform.EnglishEdition, Volume: "选择性必修第一册", SourceKind: "exercise", Confidence: "medium", Evidence: []classificationEvidence{{1, strings.Join(quote[:4], " ")}}, Notes: "Test recognition proposal; administrator must confirm the edition."}
	badEvidence := false
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body["messages"].([]any)[0].(map[string]any)["content"].(string), "source_kind") {
			t.Error("classification was not separated from chapter extraction")
		}
		out := proposal
		if badEvidence {
			out.Evidence = []classificationEvidence{{99, "fabricated contents"}}
		}
		text, _ := json.Marshal(out)
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(text)}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: http.Header{}}, nil
	})}
	input := map[string]any{"title": "Unclassified original fixture", "question_path": "q.pdf", "answer_path": "a.pdf", "admin_password": "learning-test-password"}
	result := admin.call("POST", "/api/v1/admin/learning/local-import", input, 202)
	id := rawString(result, "id")
	var confirmed bool
	var hasSubject bool
	if err = s.db.QueryRow(`SELECT scope_confirmed,subject_id IS NOT NULL FROM learning_documents WHERE id=$1`, id).Scan(&confirmed, &hasSubject); err != nil || confirmed || hasSubject {
		t.Fatal("unknown document silently assigned", err)
	}
	scope := map[string]any{"subject_id": "english", "edition": platform.EnglishEdition, "volume": "选择性必修第一册", "admin_password": "learning-test-password"}
	admin.call("POST", "/api/v1/admin/learning/documents/"+id+"/scope", scope, 409)
	student.call("POST", "/api/v1/admin/learning/documents/"+id+"/scope", scope, 403)
	if err = s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := admin.call("GET", "/api/v1/learning/jobs/"+rawString(result, "job_id"), nil, 200)
	if rawString(status, "status") != "completed" {
		t.Fatal(status)
	}
	if err = s.db.QueryRow(`SELECT scope_confirmed,subject_id IS NOT NULL FROM learning_documents WHERE id=$1`, id).Scan(&confirmed, &hasSubject); err != nil || confirmed || hasSubject {
		t.Fatal("AI suggestion became approved metadata", err)
	}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	if _, err = s.outlinePDF(ctx, id); err == nil {
		t.Fatal("chapter extraction bypassed confirmation")
	}
	if err = s.validateChapterPages(ctx, id, []chapterSelection{{Key: "u1", Title: "Unit1", QuestionPages: []int{1}, AnswerPages: []int{1}}}); err == nil {
		t.Fatal("question extraction bypassed confirmation")
	}
	badEvidence = true
	if _, err = s.identifyPDF(ctx, id); err == nil {
		t.Fatal("fabricated classification evidence accepted")
	}
	badEvidence = false
	scope["edition"] = "人教版2019"
	admin.call("POST", "/api/v1/admin/learning/documents/"+id+"/scope", scope, 400)
	scope["edition"] = platform.EnglishEdition
	admin.call("POST", "/api/v1/admin/learning/documents/"+id+"/scope", scope, 202)
	admin.call("POST", "/api/v1/admin/learning/documents/"+id+"/scope", scope, 409)
	var subject, edition, volume string
	if err = s.db.QueryRow(`SELECT subject_id,edition,volume,scope_confirmed FROM learning_documents WHERE id=$1`, id).Scan(&subject, &edition, &volume, &confirmed); err != nil || subject != "english" || edition != platform.EnglishEdition || volume != "选择性必修第一册" || !confirmed {
		t.Fatal(subject, edition, volume, confirmed, err)
	}
	// The physical PDFs and accounts remain untouched by metadata confirmation.
	var same bool
	s.db.QueryRow(`SELECT question_pdf=$2 AND answer_pdf=$3 FROM learning_documents WHERE id=$1`, id, q, a).Scan(&same)
	if !same {
		t.Fatal("original PDF snapshots changed")
	}
	if _, err = s.db.Exec(`INSERT INTO learning_documents(id,owner_id,title,question_pdf,answer_pdf,scope_confirmed,recognition) VALUES('wrong-edition','teacher','Conflict',$1,$2,false,'{"subject_id":"english","edition":"PEP 2019","edition_conflict":true}')`, q, a); err != nil {
		t.Fatal(err)
	}
	admin.call("POST", "/api/v1/admin/learning/documents/wrong-edition/scope", scope, 400)
	input["subject_id"] = "english"
	input["textbook"] = "人教版2019"
	admin.call("POST", "/api/v1/admin/learning/local-import", input, 400)
	input["textbook"] = platform.EnglishEdition
	manual := admin.call("POST", "/api/v1/admin/learning/local-import", input, 202)
	if err = s.db.QueryRow(`SELECT scope_confirmed FROM learning_documents WHERE id=$1`, rawString(manual, "id")).Scan(&confirmed); err != nil || confirmed {
		t.Fatal("manual hints bypassed scope confirmation", err)
	}
}

func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestTextbookVolumeFiltersDoNotMixRepeatedUnits(t *testing.T) {
	s, student, _ := learningTestServer(t)
	one := learningInsertQuestion(t, s, "english", "reading")
	two := learningInsertQuestion(t, s, "english", "reading")
	if _, err := s.db.Exec(`UPDATE learning_chapters SET edition=$1,volume='选择性必修第一册',curriculum_chapter='Unit 1 Relationships' WHERE id='ch-english'`, platform.EnglishEdition); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO learning_chapters(id,subject_id,title,edition,volume,curriculum_chapter) VALUES('second-volume','english','Unit 1',$1,'必修第一册','Unit 1 Life Choices')`, platform.EnglishEdition); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE learning_questions SET chapter_id='second-volume' WHERE id=$1`, two); err != nil {
		t.Fatal(err)
	}
	for i, test := range []struct{ volume, want string }{{"选择性必修第一册", one}, {"必修第一册", two}} {
		out := student.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "english", "kind": "reading", "edition": platform.EnglishEdition, "volume": test.volume, "request_id": "curriculum-filter-" + jsonNumber(int64(i))}, 200)
		var question struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(out["question"], &question); err != nil {
			t.Fatal(err)
		}
		if question.ID != test.want {
			t.Fatal("repeated Unit1 crossed textbook volumes", out)
		}
	}
	student.call("POST", "/api/v1/learning/attempts", map[string]any{"subject_id": "english", "kind": "reading", "edition": "人教版", "request_id": "curriculum-foreign-edition"}, 400)
	catalog := student.call("GET", "/api/v1/learning/catalog", nil, 200)
	var found bool
	var subjects []struct {
		ID         string                     `json:"id"`
		Counts     map[string]int             `json:"published_counts"`
		Curriculum platform.CurriculumProfile `json:"curriculum"`
	}
	if err := json.Unmarshal(catalog["subjects"], &subjects); err != nil {
		t.Fatal(err)
	}
	for _, sub := range subjects {
		if sub.ID == "english" {
			found = true
			if sub.Counts["reading"] != 2 || !sub.Curriculum.EditionLocked {
				t.Fatal(sub)
			}
		}
	}
	if !found {
		t.Fatal("English curriculum missing")
	}
}
