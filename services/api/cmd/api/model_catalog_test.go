package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCatalogDiscoveryAndPaginationBoundary(t *testing.T) {
	calls := 0
	s := &server{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer secret" || r.URL.Host != "models.example.com" {
			t.Fatal("unsafe discovery request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"vendor/one","context_length":64000,"architecture":{"input_modalities":["text","image"]}},{"id":"vendor/two"}],"links":{"next":""}}`))}, nil
	})}}
	got, err := s.discoverModels(context.Background(), aiModel{baseURL: "https://models.example.com/v1", key: "secret"})
	if err != nil || calls != 1 || len(got) != 2 || got[0].Context == nil || got[1].Context != nil || got[1].Pricing != nil {
		t.Fatal(got, err, calls)
	}
	for _, next := range []string{"https://evil.example.com/v1/models", "https://models.example.com/other", "http://models.example.com/v1/models", "https://secret@models.example.com/v1/models"} {
		if _, err := catalogNext("https://models.example.com/v1/models", "https://models.example.com/v1/models", next); err == nil {
			t.Fatal("unsafe next page accepted", next)
		}
	}
	if _, err := catalogNext("https://models.example.com/v1/models", "https://models.example.com/v1/models", "?offset=100"); err != nil {
		t.Fatal(err)
	}
}
func TestRoutePolicyAndTransientConcurrency(t *testing.T) {
	p := defaultRoutePolicy()
	if p.validate() != nil {
		t.Fatal("default invalid")
	}
	p.Attempts = 100
	if p.validate() == nil {
		t.Fatal("unbounded attempts accepted")
	}
	releases := []func(){}
	for i := 0; i < 6; i++ {
		f, e := takeAISlot("concurrency-fixture")
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, f)
	}
	if _, err := takeAISlot("concurrency-fixture"); err == nil {
		t.Fatal("unbounded concurrency")
	}
	for _, f := range releases {
		f()
	}
	f, err := takeAISlot("concurrency-fixture")
	if err != nil {
		t.Fatal("slot became quota", err)
	}
	f()
}
func TestTaskFallbackRechecksAuthorization(t *testing.T) {
	s, _, _ := learningTestServer(t)
	first := learningTestModel(t, s, "route-first")
	second := learningTestModel(t, s, "route-second")
	ids, _ := json.Marshal([]int64{first, second})
	p := defaultRoutePolicy()
	p.Fallback = true
	raw, _ := json.Marshal(p)
	if _, err := s.db.Exec(`UPDATE learning_ai_tasks SET model_ids=$1,routing=$2 WHERE task='tutor'`, string(ids), string(raw)); err != nil {
		t.Fatal(err)
	}
	seen := []string{}
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, body.Model)
		code := 200
		response := `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`
		if body.Model == "route-first" {
			code = 503
			response = `{}`
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	ctx := context.WithValue(context.Background(), identityKey{}, identity{ID: "demo", Role: "learner"})
	models, _, err := s.taskModels(ctx, "tutor", false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := s.learningCompletion(ctx, models[0], "test", "test")
	if err != nil || text != "ok" || len(seen) != 2 {
		t.Fatal(text, err, seen)
	}
	s.db.Exec(`DELETE FROM group_models WHERE model_id=$1`, second)
	seen = nil
	if _, err = s.learningCompletion(ctx, models[0], "test", "test"); err == nil || len(seen) != 1 {
		t.Fatal("revoked fallback called", seen, err)
	}
}
