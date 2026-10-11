package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Set TEST_DATABASE_URL to a disposable PostgreSQL instance. Each run uses its
// own schema, including both migrations; it never resets a shared database.
func TestStudyLoopIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("els_test_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	values := u.Query()
	values.Set("search_path", schema)
	u.RawQuery = values.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir, err := filepath.Abs("../../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIGRATIONS_DIR", dir)
	if err = migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, db); err != nil {
		t.Fatalf("migration replay: %v", err)
	}
	server := &server{db: db, origin: "http://localhost:3000"}
	token := security.Token(32)
	if _, err = db.ExecContext(ctx, `UPDATE users SET username='study-tester',password_hash=$1 WHERE id='demo'`, security.HashPassword("study-test-long-password")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,'demo',now()+interval '1 day')`, security.Digest(token)); err != nil {
		t.Fatal(err)
	}
	authorize := func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: "els_session", Value: token})
		request.Header.Set("X-ELS-Client", "web")
		request.Header.Set("X-CSRF-Token", security.CSRF(token))
	}
	handler := server.routes()
	call := func(method, path string, body any, want int) map[string]json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		record := httptest.NewRecorder()
		authorize(request)
		handler.ServeHTTP(record, request)
		if record.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, record.Code, want, record.Body.String())
		}
		var response map[string]json.RawMessage
		if err := json.Unmarshal(record.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	sessionPath := func(id int64) string { return fmt.Sprintf("/api/v1/study/sessions/%d", id) }
	getSession := func(id int64) studySession {
		t.Helper()
		raw, _ := json.Marshal(call("GET", sessionPath(id), nil, 200))
		var ss studySession
		if err := json.Unmarshal(raw, &ss); err != nil {
			t.Fatal(err)
		}
		return ss
	}
	start := func(mode, kind string, limit int) int64 {
		t.Helper()
		response := call("POST", "/api/v1/study/sessions", map[string]any{"mode": mode, "types": []string{kind}, "limit": limit, "new_limit": 5}, 201)
		var id int64
		json.Unmarshal(response["id"], &id)
		return id
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	expected := func(q int64) string {
		t.Helper()
		var answer string
		if err := db.QueryRowContext(ctx, `SELECT CASE WHEN q.question_type='spelling' THEN w.lemma ELSE ws.meaning_zh END FROM study_questions q JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id WHERE q.id=$1`, q).Scan(&answer); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	nextID := 0
	finish := func(id int64, q question, answer string, hint bool, rating int, resolution string) {
		t.Helper()
		nextID++
		call("POST", sessionPath(id)+"/answers", answerInput{q.ID, fmt.Sprintf("answer-test-%d", nextID), answer, 1200, hint}, 200)
		call("POST", sessionPath(id)+"/review", reviewInput{q.ID, fmt.Sprintf("review-test-%d", nextID), rating, resolution}, 200)
	}

	call("POST", "/api/v1/study/sessions", map[string]any{"mode": "invalid"}, 400)
	call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"spelling", "spelling"}}, 400)
	call("POST", "/api/v1/study/sessions", map[string]any{"new_limit": 0}, 200)
	if count(`SELECT COUNT(*) FROM study_sessions`) != 0 {
		t.Fatal("empty queue persisted an unusable session")
	}
	id := start("scheduled", "spelling", 20)
	ss := getSession(id)
	if len(ss.Questions) != 5 {
		t.Fatalf("one question per sense: %d", len(ss.Questions))
	}
	q := ss.Questions[0]
	if q.Expected != "" || q.Example != "" {
		t.Fatal("unsubmitted question exposed its answer")
	}
	resumed := call("POST", "/api/v1/study/sessions", map[string]any{"mode": "practice"}, 200)
	if string(resumed["id"]) != fmt.Sprint(id) || string(resumed["resumed"]) != "true" {
		t.Fatal("created duplicate active session")
	}
	submitted := answerInput{q.ID, "duplicate-answer-0001", "wrong-spelling", 1200, false}
	call("POST", sessionPath(id)+"/answers", submitted, 200)
	replay := call("POST", sessionPath(id)+"/answers", submitted, 200)
	if string(replay["replayed"]) != "true" {
		t.Fatal("answer retry not idempotent")
	}
	if count(`SELECT COUNT(*) FROM review_states`) != 0 {
		t.Fatal("unrated answer changed schedule")
	}
	restored := getSession(id).Questions[0]
	if restored.Expected == "" || restored.Result == nil || restored.Rating != nil || len(restored.Intervals) != 4 {
		t.Fatal("feedback stage cannot be restored")
	}
	call("POST", sessionPath(id)+"/answers", answerInput{ss.Questions[1].ID, "out-of-order-0001", "guess", 0, false}, 409)
	call("POST", sessionPath(id)+"/review", reviewInput{q.ID, "wrong-review-0001", 3, ""}, 400)
	rated := reviewInput{q.ID, "again-review-0001", 1, ""}
	call("POST", sessionPath(id)+"/review", rated, 200)
	call("POST", sessionPath(id)+"/review", rated, 200)
	if count(`SELECT SUM(attempts) FROM review_states`) != 1 {
		t.Fatal("review retry counted twice")
	}
	call("POST", sessionPath(id)+"/undo", map[string]string{"request_id": "undo-question-0001"}, 200)
	call("POST", sessionPath(id)+"/undo", map[string]string{"request_id": "undo-question-0001"}, 200)
	if count(`SELECT COUNT(*) FROM review_states`) != 0 || getSession(id).Questions[0].Result != nil {
		t.Fatal("undo did not restore initial state")
	}
	call("POST", sessionPath(id)+"/answers", submitted, 409) // an undone request remains reserved in the audit log
	finish(id, q, expected(q.ID), false, 4, "")
	for _, item := range ss.Questions[1:] {
		finish(id, item, expected(item.ID), false, 4, "")
	}
	if count(`SELECT COUNT(*) FROM review_states WHERE fsrs_card IS NOT NULL`) != 5 {
		t.Fatal("missing FSRS memory")
	}
	empty := call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"spelling"}, "new_limit": 0}, 200)
	if string(empty["id"]) != "null" {
		t.Fatal("non-due card scheduled early")
	}
	buried := call("POST", "/api/v1/study/sessions", map[string]any{"types": []string{"meaning_recall"}}, 200)
	if string(buried["id"]) != "null" {
		t.Fatal("sibling answers revealed on the same day")
	}
	practice := start("practice", "meaning_recall", 1)
	p := getSession(practice).Questions[0]
	stateBefore := count(`SELECT SUM(attempts) FROM review_states`)
	call("POST", sessionPath(practice)+"/answers", answerInput{p.ID, "pending-answer-0001", "我的近义释义", 1200, false}, 200)
	call("POST", sessionPath(practice)+"/review", reviewInput{p.ID, "pending-rating-0001", 3, ""}, 400)
	call("POST", sessionPath(practice)+"/review", reviewInput{p.ID, "pending-rating-0001", 3, "correct"}, 200)
	if count(`SELECT SUM(attempts) FROM review_states`) != stateBefore {
		t.Fatal("practice changed formal schedule")
	}
	if count(`SELECT COUNT(*) FROM answer_logs WHERE initial_result='pending' AND resolved_result='correct'`) != 1 {
		t.Fatal("manual grading source not retained")
	}

	// Force an existing ability due. Undo must restore every byte of its card,
	// its previous due date and its cumulative counters, not just the question.
	if _, err = db.ExecContext(ctx, `UPDATE review_states SET next_review_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	dueSession := start("scheduled", "spelling", 1)
	d := getSession(dueSession).Questions[0]
	snapshotSQL := `SELECT row_to_json(rs)::text FROM review_states rs JOIN study_questions q ON q.sense_id=rs.sense_id AND q.question_type=rs.dimension WHERE q.id=$1`
	var before, after string
	if err = db.QueryRowContext(ctx, snapshotSQL, d.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	finish(dueSession, d, expected(d.ID), true, 1, "")
	call("POST", sessionPath(dueSession)+"/undo", map[string]string{"request_id": "restore-memory-0001"}, 200)
	if err = db.QueryRowContext(ctx, snapshotSQL, d.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("undo changed existing memory:\n%s\n%s", before, after)
	}
	finish(dueSession, d, expected(d.ID), false, 3, "")

	// Concurrent creation must return the same active session, without orphan
	// sessions or duplicate questions. Both requests use the real SQL locks.
	var wg sync.WaitGroup
	records := make([]*httptest.ResponseRecorder, 2)
	for i := range records {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			records[i] = httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/api/v1/study/sessions", bytes.NewBufferString(`{"mode":"practice","types":["spelling"],"limit":1}`))
			authorize(request)
			handler.ServeHTTP(records[i], request)
		}(i)
	}
	wg.Wait()
	var firstID int64
	for i, record := range records {
		if record.Code != 200 && record.Code != 201 {
			t.Fatalf("concurrent start: %s", record.Body.String())
		}
		var out struct{ ID int64 }
		json.Unmarshal(record.Body.Bytes(), &out)
		if i == 0 {
			firstID = out.ID
		} else if out.ID != firstID {
			t.Fatal("multiple active sessions created")
		}
	}
	call("GET", "/api/v1/dashboard", nil, 200)
	call("GET", "/api/v1/study/sessions/active", nil, 200)
	call("GET", "/api/v1/study/sessions/999999", nil, 404)
}
