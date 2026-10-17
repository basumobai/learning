package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Original synthetic fixtures exercise pipeline contracts; these are not actual
// textbooks or official papers and do not measure real-provider accuracy.
func syntheticPDF(lines ...string) []byte {
	var stream strings.Builder
	stream.WriteString("BT /F1 12 Tf 50 780 Td ")
	for i, line := range lines {
		if i > 0 {
			stream.WriteString("0 -18 Td ")
		}
		line = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(line)
		fmt.Fprintf(&stream, "(%s) Tj ", line)
	}
	stream.WriteString("ET")
	objects := []string{`<< /Type /Catalog /Pages 2 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 820] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", stream.Len(), stream.String())}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", len(objects)+1, xref)
	return pdf.Bytes()
}
func TestResourceAcceptanceContentMatrix(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("Poppler required")
	}
	s, _, _ := learningTestServer(t)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	model := learningTestModel(t, s, "matrix-classifier")
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1 WHERE task='classify_document'`, "["+jsonNumber(model)+"]")
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		result := lightResult{Subjects: []string{"english"}, Type: "textbook", Alignment: "unknown", Edition: "北师大版2019", Confidence: .9, Evidence: []classificationEvidence{{1, "Beijing Normal University 2019"}}, Notes: "Synthetic fixture: textbook publisher identified; volume remains unconfirmed."}
		if bytes.Contains(raw, []byte("Mock Examination")) {
			year := 2025
			result = lightResult{Subjects: []string{"english"}, Type: "mock_paper", Alignment: "no", Year: &year, Region: "广东", Confidence: .9, Evidence: []classificationEvidence{{1, "Mock Examination 2025"}}, Notes: "Actual sampled header says mock; filename cannot establish authenticity."}
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": stringMustMarshal(result)}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	})}
	tests := []struct {
		name, title, text, subject, kind string
		multi                            bool
	}{
		{"1_textbook", "北师大版高中英语选择性必修一.pdf", "Beijing Normal University 2019\nUnit 1 Relationships\nEnglish textbook reading and vocabulary.", "english", "textbook", false},
		{"2_grammar", "高中英语语法综合练习.pdf", "Relative clauses\nNon-finite verbs\nSubjunctive mood\nChoose the correct form in these grammar exercises.", "english", "grammar_practice", true},
		{"3_exam_authenticity", "2025广东高考英语真题.pdf", "Mock Examination 2025\nEnglish examination practice paper.\nReading comprehension and language use.", "english", "mock_paper", false},
		{"4_physics_topic", "高中物理电磁感应专题.pdf", "Electromagnetic induction\nMagnetic flux and induced emf\nCalculate the induced emf in the closed circuit.", "physics", "knowledge_topic", false},
		{"5_wrong_filename", "英语练习.pdf", "Mathematics calculus\nDerivative and parabola\nCalculate the derivative and analyze the parabola.", "math", "knowledge_topic", false},
		{"6_mixed_topics", "综合专项.pdf", "Relative clauses and non-finite verbs\nReading comprehension\nVocabulary practice\nMultiple English learning topics in this document.", "english", "comprehensive", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pdf := syntheticPDF(strings.Split(test.text, "\n")...)
			id, err := s.storeRawResource(ctx, test.title, pdf, nil, resourceSource{})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(resourceRequest{Document: id, Revision: 1})
			out, err := s.classifyResource(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			var stage, subject, kind, edition string
			var classification []byte
			if err = s.db.QueryRow(`SELECT stage,subject_id,resource_type,edition,classification FROM learning_documents WHERE id=$1`, id).Scan(&stage, &subject, &kind, &edition, &classification); err != nil {
				t.Fatal(err)
			}
			var result lightResult
			json.Unmarshal(classification, &result)
			if stage != "B" || subject != test.subject || kind != test.kind || result.Multi != test.multi {
				t.Fatal(stage, subject, kind, result, stringMustMarshal(out))
			}
			if test.kind != "textbook" && edition != "" {
				t.Fatal("independent material forced into a textbook")
			}
			if result.Authenticity != "unverified" {
				t.Fatal("filename confirmed authenticity")
			}
			var pages int
			s.db.QueryRow(`SELECT count(*) FROM learning_pages WHERE document_id=$1`, id).Scan(&pages)
			if pages != 0 {
				t.Fatal("A to B performed full preparation")
			}
			got, _ := s.documentPDF(ctx, id, "question")
			if !bytes.Equal(pdf, got) {
				t.Fatal("original changed")
			}
		})
	}
}
func TestLegacyOriginalMigrationAndBatchCAS(t *testing.T) {
	s, _, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	for _, id := range []string{"legacy-1", "legacy-2"} {
		if _, err := s.db.Exec(`INSERT INTO learning_documents(id,owner_id,title,question_pdf,answer_pdf,subject_id) VALUES($1,'teacher',$1,$2,$2,'english')`, id, pdf); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	if _, err := s.migrateLegacyOriginals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.migrateLegacyOriginals(ctx); err != nil {
		t.Fatal(err)
	}
	var originals, aliases int
	s.db.QueryRow(`SELECT count(*),count(duplicate_of) FROM learning_documents WHERE id LIKE 'legacy-%' AND question_pdf=$1`, pdf).Scan(&originals, &aliases)
	if originals != 2 || aliases != 1 {
		t.Fatal("legacy data lost", originals, aliases)
	}
	id, err := s.storeRawResource(ctx, "same.pdf", pdf, pdf, resourceSource{})
	if err != nil || id != "legacy-1" {
		t.Fatal("existing content not reused", id, err)
	}
	input := map[string]any{"resources": []reviewSelection{{"legacy-1", 1}, {"legacy-2", 9}}, "kind": "topic", "names": []string{"统一标签"}}
	admin.call("POST", "/api/v1/admin/resources/tags/batch", input, 409)
	var revision int
	s.db.QueryRow(`SELECT revision FROM learning_documents WHERE id='legacy-1'`).Scan(&revision)
	if revision != 1 {
		t.Fatal("partial batch committed")
	}
	input["resources"] = []reviewSelection{{"legacy-1", 1}, {"legacy-2", 1}}
	admin.call("POST", "/api/v1/admin/resources/tags/batch", input, 200)
	admin.call("GET", "/api/v1/admin/resources?source=legacy&confidence=0&duplicates=true", nil, 200)
}
func TestOCRFailureLeavesUncertainOriginal(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("Poppler required")
	}
	s, _, _ := learningTestServer(t)
	model := learningTestModel(t, s, "ocr-failure")
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1 WHERE task='ocr'`, "["+jsonNumber(model)+"]")
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
	})}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf := syntheticPDF("")
	id, err := s.storeRawResource(ctx, "英语练习.pdf", pdf, nil, resourceSource{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resourceRequest{Document: id, Revision: 1})
	if _, err = s.classifyResource(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var stage, state string
	var c []byte
	s.db.QueryRow(`SELECT stage,classification_status,classification FROM learning_documents WHERE id=$1`, id).Scan(&stage, &state, &c)
	if stage != "A" || state != "needs_confirmation" || !strings.Contains(string(c), "OCR失败") {
		t.Fatal(stage, state, string(c))
	}
}
func TestTaskPoolSkipsDisabledPreferredModel(t *testing.T) {
	s, _, _ := learningTestServer(t)
	first := learningTestModel(t, s, "disabled-preferred")
	second := learningTestModel(t, s, "active-fallback")
	policy := defaultRoutePolicy()
	policy.Fallback = true
	s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1,routing=$2 WHERE task='tutor'`, stringMustMarshal([]int64{first, second}), nullableJSON(policy))
	s.db.Exec(`UPDATE ai_models SET enabled=false WHERE id=$1`, first)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "demo", Role: "learner"})
	models, _, err := s.taskModels(ctx, "tutor", false)
	if err != nil || len(models) != 1 || models[0].ID != second {
		t.Fatal("disabled preferred prevented fallback", models, err)
	}
}
