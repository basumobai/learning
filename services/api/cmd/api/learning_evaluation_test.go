package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEvaluationRequiresHumanReviewForQualityGate(t *testing.T) {
	t.Setenv("LEARNING_V3_QUALITY_GATE", "true")
	s, _, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	model := learningTestModel(t, s, "evaluation-fixture")
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "teacher", Role: "admin"})
	prompt := "Explain the concept clearly."
	if s.requireModelEvaluation(ctx, "guided_tutor", prompt, []int64{model}) == nil {
		t.Fatal("unevaluated model approved")
	}
	sample := admin.call("POST", "/api/v1/admin/learning/evaluations/cases", map[string]any{"subject_id": "physics", "task": "guided_tutor", "input": "Synthetic concept fixture.", "expected_terms": []string{"concept"}, "rights_note": "Original synthetic test content, no external teaching material."}, 201)
	input := map[string]any{"case_id": rawString(sample, "id"), "model_id": model, "prompt": prompt, "request_id": "evaluation-run-one"}
	run := admin.call("POST", "/api/v1/admin/learning/evaluations", input, 202)
	id := rawString(run, "id")
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"A concept explanation for the synthetic fixture."},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":10}}`))}, nil
	})}
	if err := s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.requireModelEvaluation(ctx, "guided_tutor", prompt, []int64{model}) == nil {
		t.Fatal("automatic check bypassed human review")
	}
	admin.call("POST", "/api/v1/admin/learning/evaluations/"+id+"/review", map[string]string{"decision": "approved", "note": "Human checked the original fixture and the concept explanation."}, 200)
	if err := s.requireModelEvaluation(ctx, "guided_tutor", prompt, []int64{model}); err != nil {
		t.Fatal(err)
	}
	if s.requireModelEvaluation(ctx, "guided_tutor", prompt+" changed", []int64{model}) == nil {
		t.Fatal("new prompt reused old approval")
	}
}
