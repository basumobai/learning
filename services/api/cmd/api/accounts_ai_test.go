package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/els/api/internal/security"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("els_accounts_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { db.Close(); admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	dir, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIGRATIONS_DIR", dir)
	if err = migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

type apiTester struct {
	t       *testing.T
	handler http.Handler
	token   string
	csrf    string
}

func (c *apiTester) request(method, path string, body any) *http.Request {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-ELS-Client", "web")
	if c.token != "" {
		r.AddCookie(&http.Cookie{Name: "els_session", Value: c.token})
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	return r
}
func (c *apiTester) raw(method, path string, body any, want int) *httptest.ResponseRecorder {
	c.t.Helper()
	record := httptest.NewRecorder()
	c.handler.ServeHTTP(record, c.request(method, path, body))
	if record.Code != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, record.Code, want, record.Body.String())
	}
	return record
}
func (c *apiTester) call(method, path string, body any, want int) map[string]json.RawMessage {
	c.t.Helper()
	r := c.raw(method, path, body, want)
	var result map[string]json.RawMessage
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		c.t.Fatal(err)
	}
	return result
}
func (c *apiTester) login(username, password, code string) {
	c.t.Helper()
	r := c.raw("POST", "/api/v1/auth/login", map[string]string{"username": username, "password": password, "code": code}, 200)
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		c.t.Fatal("insecure session cookie")
	}
	c.token = cookies[0].Value
	me := c.call("GET", "/api/v1/auth/me", nil, 200)
	if err := json.Unmarshal(me["csrf_token"], &c.csrf); err != nil {
		c.t.Fatal(err)
	}
}
func rawInt(m map[string]json.RawMessage, key string) int64 {
	var v int64
	_ = json.Unmarshal(m[key], &v)
	return v
}
func rawString(m map[string]json.RawMessage, key string) string {
	var v string
	_ = json.Unmarshal(m[key], &v)
	return v
}

func TestAccountsAIAndLearningIsolation(t *testing.T) {
	db := testDatabase(t)
	password := "administrator-testing-password"
	t.Setenv("ELS_ADMIN_USERNAME", "admin")
	t.Setenv("ELS_ADMIN_PASSWORD", password)
	var chatCalls, audioCalls atomic.Int64
	transcript := "On Monday, Lily planned to meet her friend at the library at three o'clock. However, the library was closed for repairs. They decided to meet at the park instead. Lily brought a book and they talked about their school project before going home."
	s := &server{db: db, origin: "http://localhost:3000", encryptionKey: bytes.Repeat([]byte{7}, 32)}
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer test-provider-secret" {
			t.Error("provider key missing")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/v1/audio/speech" {
			audioCalls.Add(1)
			if rawString(body, "model") != "tts-test" || rawString(body, "voice") != "cedar" {
				t.Error("speech model or voice changed")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/mpeg"}}, Body: io.NopCloser(strings.NewReader("ID3-test-audio"))}, nil
		}
		chatCalls.Add(1)
		if rawString(body, "model") != "chat-test" {
			t.Error("unauthorized or fallback model sent upstream")
		}
		var messages []chatMessage
		_ = json.Unmarshal(body["messages"], &messages)
		text := "先找到主语和谓语，再解释修饰成分。"
		if strings.Contains(messages[0].Content, "将用户给出的英语句子") {
			a := sentenceAnalysis{Translation: "你借给我的书帮助了我。", MainClause: "The book helped me", Chunks: []sentenceChunk{{"The book", "主语", "说明谁产生帮助"}, {"that you lent me", "定语从句", "修饰 book"}, {"helped me", "谓语与宾语", "说明做了什么"}}, Grammar: []string{"that 引导定语从句"}, Paraphrase: "Your book helped me.", Check: sentenceCheck{"什么帮助了我？", "那本书", "The book 是主语"}}
			raw, _ := json.Marshal(a)
			text = string(raw)
		}
		if strings.Contains(messages[0].Content, "创建一份原创") {
			lesson := listeningLesson{Title: "地点改变", Transcript: transcript, Translation: "莉莉改在公园见朋友。", Questions: []listeningQuestion{{"Where did they finally meet?", []string{"The library", "The park", "The school", "At home"}, 1, "图书馆关闭后改在公园。", "They decided to meet at the park instead."}}}
			raw, _ := json.Marshal(lesson)
			text = string(raw)
		}
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}
	if err := s.bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := s.routes()
	a := &apiTester{t: t, handler: handler}
	guest := &apiTester{t: t, handler: handler}
	guest.call("GET", "/api/v1/dashboard", nil, 401)
	attack := guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": password})
	attack.Header.Set("Origin", "https://attacker.example")
	record := httptest.NewRecorder()
	handler.ServeHTTP(record, attack)
	if record.Code != 403 {
		t.Fatal("cross-origin login accepted")
	}
	a.login("admin", password, "")
	a.call("GET", "/api/v1/dashboard", nil, 403)
	a.call("POST", "/api/v1/auth/password", map[string]string{"current_password": password, "new_password": "administrator-permanent-password"}, 200)
	password = "administrator-permanent-password"
	a.login("admin", password, "")
	missingCSRF := a.request("POST", "/api/v1/auth/revoke-sessions", map[string]any{})
	missingCSRF.Header.Del("X-CSRF-Token")
	record = httptest.NewRecorder()
	handler.ServeHTTP(record, missingCSRF)
	if record.Code != 403 {
		t.Fatal("CSRF bypass accepted")
	}
	provider := a.call("POST", "/api/v1/admin/providers", map[string]any{"name": "Test", "base_url": "https://provider.example.com/v1", "api_key": "test-provider-secret", "enabled": true, "admin_password": password}, 200)
	pid := rawString(provider, "id")
	model := func(slug, kind string) int64 {
		return rawInt(a.call("POST", "/api/v1/admin/models", map[string]any{"provider_id": pid, "model_id": slug, "name": slug, "kind": kind, "voices": []string{"cedar", "marin"}, "json_mode": true, "enabled": true, "admin_password": password}, 200), "id")
	}
	chat, tts, forbidden := model("chat-test", "chat"), model("tts-test", "tts"), model("forbidden-test", "chat")
	group := rawInt(a.call("POST", "/api/v1/admin/groups", map[string]any{"name": "学习组", "model_ids": []int64{}, "admin_password": password}, 200), "id")
	a.call("POST", "/api/v1/admin/groups", map[string]any{"id": 1, "name": "基础学习", "model_ids": []int64{chat, tts}, "admin_password": password}, 200)
	config := a.raw("GET", "/api/v1/admin/config", nil, 200)
	if strings.Contains(config.Body.String(), "test-provider-secret") || strings.Contains(config.Body.String(), "password_hash") {
		t.Fatal("admin config leaked credentials")
	}
	var encrypted []byte
	if err := db.QueryRow(`SELECT api_key FROM ai_providers WHERE id=$1`, pid).Scan(&encrypted); err != nil || bytes.Contains(encrypted, []byte("test-provider-secret")) {
		t.Fatal("provider key stored in plaintext")
	}
	studentPassword := "student-initial-password"
	studentID := rawString(a.call("POST", "/api/v1/admin/users", map[string]any{"username": "student", "display_name": "学习用户", "role": "learner", "group_id": group, "password": studentPassword, "admin_password": password}, 200), "id")
	u := &apiTester{t: t, handler: handler}
	u.login("student", studentPassword, "")
	u.call("GET", "/api/v1/dashboard", nil, 403)
	u.call("POST", "/api/v1/auth/password", map[string]string{"current_password": studentPassword, "new_password": "student-permanent-password"}, 200)
	u.call("GET", "/api/v1/auth/me", nil, 401)
	studentPassword = "student-permanent-password"
	u.login("student", studentPassword, "")
	u.call("GET", "/api/v1/admin/config", nil, 403)
	conv := rawString(u.call("POST", "/api/v1/ai/conversations", map[string]string{"title": "学习句子"}, 201), "id")
	message := map[string]any{"model_id": chat, "request_id": "chat-learning-0001", "content": "每个词都认识，但读不懂整句。"}
	u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 403)
	if chatCalls.Load() != 0 {
		t.Fatal("denied model called upstream")
	}
	a.call("POST", "/api/v1/admin/groups", map[string]any{"id": group, "name": "学习组", "model_ids": []int64{chat, tts}, "admin_password": password}, 200)
	u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 200)
	u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 200)
	if chatCalls.Load() != 1 {
		t.Fatal("chat retry paid upstream twice")
	}
	message["content"] = "different"
	u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 409)
	message["model_id"] = forbidden
	u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 403)
	adminConv := rawString(a.call("POST", "/api/v1/ai/conversations", map[string]string{"title": "private-admin"}, 201), "id")
	u.call("GET", "/api/v1/ai/conversations/"+adminConv, nil, 404)
	message["model_id"] = chat
	message["request_id"] = "cross-account-chat"
	u.call("POST", "/api/v1/ai/conversations/"+adminConv+"/messages", message, 404)
	// Unlimited application use: repeated authorized calls have no conversation/call budget.
	for i := 0; i < 12; i++ {
		message["request_id"] = fmt.Sprintf("unlimited-chat-%03d", i)
		u.call("POST", "/api/v1/ai/conversations/"+conv+"/messages", message, 200)
	}
	sentence := u.call("POST", "/api/v1/ai/training", map[string]any{"model_id": chat, "request_id": "sentence-learning-0001", "kind": "sentence", "text": "The book that you lent me helped me.", "level": ""}, 200)
	u.call("GET", "/api/v1/ai/training/"+rawString(sentence, "id"), nil, 200)
	a.call("GET", "/api/v1/ai/training/"+rawString(sentence, "id"), nil, 404)
	lesson := u.call("POST", "/api/v1/ai/training", map[string]any{"model_id": chat, "request_id": "listening-learning-0001", "kind": "listening", "text": "地点变化", "level": "A2"}, 200)
	lid := rawString(lesson, "id")
	public, _ := json.Marshal(lesson)
	if strings.Contains(string(public), "transcript") || strings.Contains(string(public), "evidence") || strings.Contains(string(public), "explanation") {
		t.Fatal("listening leaked answers before submission")
	}
	speech := map[string]any{"model_id": tts, "voice": "cedar"}
	u.raw("POST", "/api/v1/ai/training/"+lid+"/speech", speech, 200)
	u.raw("POST", "/api/v1/ai/training/"+lid+"/speech", speech, 200)
	if audioCalls.Load() != 1 {
		t.Fatal("replay audio did not use cache")
	}
	a.raw("POST", "/api/v1/ai/training/"+lid+"/speech", speech, 404)
	exported := u.raw("GET", "/api/v1/account/export", nil, 200)
	if strings.Contains(exported.Body.String(), transcript) || strings.Contains(exported.Body.String(), "private-admin") || strings.Contains(exported.Body.String(), "test-provider-secret") {
		t.Fatal("export leaked unanswered listening or another account")
	}
	attempt := map[string]any{"request_id": "listening-answer-0001", "answers": []int{1}}
	feedback := u.call("POST", "/api/v1/ai/training/"+lid+"/answers", attempt, 200)
	if rawInt(feedback, "score") != 1 || rawString(feedback, "transcript") != transcript {
		t.Fatal("listening feedback incorrect")
	}
	u.call("POST", "/api/v1/ai/training/"+lid+"/answers", attempt, 200)
	attempt["answers"] = []int{0}
	u.call("POST", "/api/v1/ai/training/"+lid+"/answers", attempt, 409)
	a.call("POST", "/api/v1/admin/groups", map[string]any{"id": group, "name": "学习组", "model_ids": []int64{chat}, "admin_password": password}, 200)
	u.raw("POST", "/api/v1/ai/training/"+lid+"/speech", speech, 403)
	a.call("POST", "/api/v1/admin/groups", map[string]any{"id": group, "name": "学习组", "model_ids": []int64{chat, tts}, "admin_password": password}, 200)
	prefs := map[string]any{"daily_new": 1, "retention": .95, "timezone": "UTC", "tts_model_id": tts, "voice": "cedar"}
	u.call("POST", "/api/v1/preferences", prefs, 200)
	sid := rawInt(u.call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"spelling"}, "limit": 5, "new_limit": 5}, 201), "id")
	var ss studySession
	raw, _ := json.Marshal(u.call("GET", fmt.Sprintf("/api/v1/study/sessions/%d", sid), nil, 200))
	_ = json.Unmarshal(raw, &ss)
	if len(ss.Questions) != 1 {
		t.Fatal("daily new budget ignored")
	}
	qid := ss.Questions[0].ID
	a.call("GET", fmt.Sprintf("/api/v1/study/sessions/%d", sid), nil, 404)
	u.raw("POST", fmt.Sprintf("/api/v1/study/sessions/%d/questions/%d/speech", sid, qid), speech, 403)
	var answer string
	if err := db.QueryRow(`SELECT w.lemma FROM study_questions q JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id WHERE q.id=$1`, qid).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/answers", sid), answerInput{qid, "student-answer-0001", answer, 1000, false}, 200)
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/review", sid), reviewInput{qid, "student-review-0001", 3, ""}, 200)
	empty := u.call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"spelling"}, "new_limit": 5}, 200)
	if string(empty["id"]) != "null" {
		t.Fatal("reopening sessions bypassed daily budget")
	}
	if rawInt(u.call("GET", "/api/v1/dashboard", nil, 200), "new_remaining") != 0 {
		t.Fatal("daily dashboard budget incorrect")
	}
	if rawInt(a.call("GET", "/api/v1/dashboard", nil, 200), "studied") != 0 {
		t.Fatal("learning state crossed accounts")
	}
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/undo", sid), map[string]string{"request_id": "budget-undo-0001"}, 200)
	if rawInt(u.call("GET", "/api/v1/dashboard", nil, 200), "new_remaining") != 1 {
		t.Fatal("undo did not restore daily budget")
	}
	importBody := map[string]any{"text": "adapt /əˈdæpt/ v. 适应\nability /əˈbɪləti/ n. 能力", "request_id": "vocabulary-import-0001", "overwrite_phonetic": false, "admin_password": password}
	a.call("POST", "/api/v1/admin/vocabulary/preview", importBody, 200)
	imported := a.call("POST", "/api/v1/admin/vocabulary/import", importBody, 200)
	if rawInt(imported, "added") != 1 || rawInt(imported, "skipped") != 1 {
		t.Fatal("vocabulary import or duplicate handling incorrect")
	}
	a.call("POST", "/api/v1/admin/vocabulary/import", importBody, 200)
	var sense int64
	if err := db.QueryRow(`SELECT s.id FROM word_senses s JOIN words w ON w.id=s.word_id WHERE w.lemma='ability'`).Scan(&sense); err != nil {
		t.Fatal(err)
	}
	u.call("POST", "/api/v1/vocabulary/suspend", map[string]any{"sense_id": sense, "suspended": true}, 200)
	vocabulary := u.raw("GET", "/api/v1/vocabulary?q=ability", nil, 200)
	if !strings.Contains(vocabulary.Body.String(), `"suspended":true`) {
		t.Fatal("suspension not persisted")
	}
	a.call("POST", "/api/v1/admin/vocabulary/import", map[string]any{"text": "record /ˈrekɔːd/ n. 记录\nrecord /rɪˈkɔːd/ v. 记录", "request_id": "heteronym-import-0001", "admin_password": password}, 200)
	var noun, verb string
	if err := db.QueryRow(`SELECT s.phonetic FROM word_senses s JOIN words w ON w.id=s.word_id WHERE w.lemma='record' AND s.part_of_speech='n.'`).Scan(&noun); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT s.phonetic FROM word_senses s JOIN words w ON w.id=s.word_id WHERE w.lemma='record' AND s.part_of_speech='v.'`).Scan(&verb); err != nil {
		t.Fatal(err)
	}
	if noun != "/ˈrekɔːd/" || verb != "/rɪˈkɔːd/" {
		t.Fatal("heteronym IPA overwritten across senses")
	}
	a.call("POST", "/api/v1/admin/vocabulary/import", map[string]any{"text": "English /ˈɪŋɡlɪʃ/ n. 英语", "request_id": "capitalized-import-0001", "admin_password": password}, 200)
	a.call("POST", "/api/v1/admin/vocabulary/import", map[string]any{"text": "english /ˈɪŋɡlɪʃ/ n. 英语", "request_id": "capitalized-import-0002", "admin_password": password}, 200)
	var capital string
	var copies int
	if err := db.QueryRow(`SELECT MIN(lemma),COUNT(*) FROM words WHERE lower(lemma)='english'`).Scan(&capital, &copies); err != nil || capital != "English" || copies != 1 {
		t.Fatal("capitalization or case-insensitive import uniqueness failed")
	}
	// Complete the restored spelling, then exercise dictation as a distinct FSRS dimension.
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/answers", sid), answerInput{qid, "student-answer-0002", answer, 1000, false}, 200)
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/review", sid), reviewInput{qid, "student-review-0002", 3, ""}, 200)
	prefs["daily_new"] = 5
	u.call("POST", "/api/v1/preferences", prefs, 200)
	dictation := rawInt(u.call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"listening_spelling"}, "limit": 1, "new_limit": 5}, 201), "id")
	raw, _ = json.Marshal(u.call("GET", fmt.Sprintf("/api/v1/study/sessions/%d", dictation), nil, 200))
	_ = json.Unmarshal(raw, &ss)
	dq := ss.Questions[0]
	if dq.Expected != "" || dq.Phonetic != "" || dq.Prompt != "听音填写单词" {
		t.Fatal("dictation leaked spelling")
	}
	var dictationWord string
	var dictationSense int64
	if err := db.QueryRow(`SELECT w.lemma,ws.id FROM study_questions q JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id WHERE q.id=$1`, dq.ID).Scan(&dictationWord, &dictationSense); err != nil {
		t.Fatal(err)
	}
	if dictationSense == sense {
		t.Fatal("suspended sense entered scheduled queue")
	}
	u.raw("POST", fmt.Sprintf("/api/v1/study/sessions/%d/questions/%d/speech", dictation, dq.ID), speech, 200)
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/answers", dictation), answerInput{dq.ID, "dictation-answer-0001", dictationWord, 1000, false}, 200)
	u.call("POST", fmt.Sprintf("/api/v1/study/sessions/%d/review", dictation), reviewInput{dq.ID, "dictation-review-0001", 3, ""}, 200)
	var dimensions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM review_states WHERE user_id=$1 AND sense_id=$2 AND dimension='listening_spelling'`, studentID, dictationSense).Scan(&dimensions); err != nil || dimensions != 1 {
		t.Fatal("dictation schedule not independent")
	}
	// MFA enrollment, replay rejection and administrator recovery are separate from model permissions.
	started := u.call("POST", "/api/v1/auth/mfa", map[string]string{"action": "start", "password": studentPassword}, 200)
	secret := rawString(started, "secret")
	code := security.TOTP(secret, time.Now().Unix()/30)
	u.call("POST", "/api/v1/auth/mfa", map[string]string{"action": "confirm", "password": studentPassword, "code": code}, 200)
	guest.call("POST", "/api/v1/auth/login", map[string]string{"username": "student", "password": studentPassword, "code": code}, 401)
	a.call("POST", "/api/v1/admin/users", map[string]any{"id": studentID, "username": "student", "display_name": "学习用户", "role": "learner", "group_id": group, "password": "recovered-student-password", "clear_mfa": true, "admin_password": password}, 200)
	u.call("GET", "/api/v1/auth/me", nil, 401)
	u.login("student", "recovered-student-password", "")
	u.call("GET", "/api/v1/dashboard", nil, 403)
}
