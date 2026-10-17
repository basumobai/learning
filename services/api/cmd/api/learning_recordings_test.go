package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecordingOwnerAndASRReplay(t *testing.T) {
	s, a, b := learningTestServer(t)
	lesson := testSpeakingLesson()
	_, err := s.db.Exec(`INSERT INTO training_sessions(id,user_id,kind,title,payload) VALUES('recording-lesson','demo','listening','fixture',$1)`, nullableJSON(lesson))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "fixture.webm")
	part.Write(append([]byte{0x1a, 0x45, 0xdf, 0xa3}, bytes.Repeat([]byte{0}, 100)...))
	form.WriteField("training_id", "recording-lesson")
	form.WriteField("task_index", "0")
	form.WriteField("request_id", "recording-upload-one")
	form.Close()
	req := a.request("POST", "/api/v1/learning/recordings", nil)
	req.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var result map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &result)
	id := rawString(result, "id")
	b.call("GET", "/api/v1/learning/recordings/"+id+"/audio", nil, 404)
	model := learningTestModel(t, s, "asr-fixture")
	s.db.Exec(`UPDATE ai_models SET kind='asr' WHERE id=$1`, model)
	calls := 0
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Error(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"I will meet you on Friday."}`))}, nil
	})}
	input := map[string]any{"request_id": "asr-transcribe-one", "model_id": model}
	a.call("POST", "/api/v1/learning/recordings/"+id+"/transcribe", input, 200)
	a.call("POST", "/api/v1/learning/recordings/"+id+"/transcribe", input, 200)
	if calls != 1 {
		t.Fatal("ASR replay charged again", calls)
	}
}
