package main

import (
	"net"
	"testing"
)

func TestProviderSSRFBoundary(t *testing.T) {
	for _, raw := range []string{"http://openrouter.ai/api/v1", "https://localhost/v1", "https://127.0.0.1/v1", "https://169.254.169.254/latest", "https://100.64.0.1/v1", "https://[::1]/v1", "https://user:secret@openrouter.ai/v1", "https://openrouter.ai:8080/v1", "https://openrouter.ai/v1?token=secret"} {
		if validateProviderURL(raw) == nil {
			t.Fatalf("unsafe provider URL accepted: %s", raw)
		}
	}
	if validateProviderURL("https://openrouter.ai/api/v1") != nil || validateProviderURL("https://api.openai.com/v1") != nil {
		t.Fatal("public compatible endpoint rejected")
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.17.0.1", "192.168.1.1", "169.254.169.254", "100.64.1.2", "::ffff:127.0.0.1", "fd00::1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("private DNS IP accepted: %s", raw)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public address rejected")
	}
}
func TestGeneratedTrainingValidation(t *testing.T) {
	lesson := listeningLesson{Title: "test", Transcript: "A", Translation: "译文", Questions: []listeningQuestion{}}
	if lesson.validate() == nil {
		t.Fatal("empty/truncated listening content accepted")
	}
	var a sentenceAnalysis
	if decodeGenerated("```json\n{\"translation\":\"译文\"}\n```", &a) != nil {
		t.Fatal("well-formed fenced JSON rejected")
	}
	if a.validate("test") == nil {
		t.Fatal("incomplete sentence accepted")
	}
	if decodeGenerated(`{"translation":"译文","unexpected":1}`, &a) == nil || decodeGenerated(`{} {}`, &a) == nil {
		t.Fatal("extra output accepted")
	}
}
