package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBoundedSamplesAndContentOverride(t *testing.T) {
	for _, total := range []int{1, 2, 6, 100, 2000} {
		p := samplePlan(total, 6)
		if len(p) > 6 {
			t.Fatal(p)
		}
		seen := map[int]bool{}
		for _, n := range p {
			if n < 1 || n > total || seen[n] {
				t.Fatal(p)
			}
			seen[n] = true
		}
		if total > 6 && !seen[(total+1)/2] {
			t.Fatal("middle sample omitted", p)
		}
	}
	for _, test := range []struct{ text, subject, kind string }{{"定语从句 非谓语动词 虚拟语气练习", "english", "grammar_practice"}, {"函数 导数 抛物线 的计算", "math", "knowledge_topic"}, {"电磁感应 磁通量 感应电动势", "physics", "knowledge_topic"}, {"定语从句 非谓语动词 阅读理解 词汇专项", "english", "comprehensive"}} {
		r, ok := rulesLight([]samplePage{{1, test.text, "native"}})
		if !ok || len(r.Subjects) != 1 || r.Subjects[0] != test.subject || r.Type != test.kind || r.Alignment != "no" {
			t.Fatal(r)
		}
		if err := r.validate([]samplePage{{1, test.text, "native"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := rulesLight([]samplePage{{1, "2025 高考 英语 定语从句 非谓语动词", "native"}}); ok {
		t.Fatal("exam authenticity guessed")
	}
	r := lightResult{Subjects: []string{"english"}, Type: "grammar_practice", Alignment: "no", Confidence: .9, Evidence: []classificationEvidence{{9, "not on page"}}}
	if r.validate([]samplePage{{1, "real text", "native"}}) == nil {
		t.Fatal("fabricated evidence accepted")
	}
}
func TestLightClassificationNeverPreparesWholePDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("Poppler required")
	}
	s, _, _ := learningTestServer(t)
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	id, err := s.storeRawResource(ctx, "英语练习.pdf", pdf, nil, resourceSource{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resourceRequest{Document: id, Revision: 1})
	_, err = s.classifyResource(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM learning_pages WHERE document_id=$1`, id).Scan(&n)
	if n != 0 {
		t.Fatal("light path performed full preparation", n)
	}
	var stage string
	s.db.QueryRow(`SELECT stage FROM learning_documents WHERE id=$1`, id).Scan(&stage)
	if stage != "A" {
		t.Fatal("uncertain document auto-classified", stage)
	}
	out, err := s.classifyResource(ctx, raw)
	if err != nil || !strings.Contains(stringMustJSON(out), `"cached":true`) {
		t.Fatal("classification cache unused", out, err)
	}
}
func stringMustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestProcessingBudgetIncludesRetries(t *testing.T) {
	b := &processingBudget{Tokens: 100}
	if b.reserveTokens(60) != nil || b.reserveTokens(60) == nil {
		t.Fatal("token budget not cumulative")
	}
	max := .1
	b.Cost = &max
	if b.reserveCost(.06) != nil || b.reserveCost(.06) == nil {
		t.Fatal("money budget not cumulative")
	}
}
