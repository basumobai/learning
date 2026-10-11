package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPhase0UpgradeIntegration(t *testing.T) {
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
	schema := fmt.Sprintf("els_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
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
	initial, err := os.ReadFile(filepath.Join(dir, "0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES('0001_init.sql');
 INSERT INTO study_sessions(user_id) VALUES('demo');
 INSERT INTO study_questions(session_id,sense_id,question_type,prompt,position) VALUES(1,1,'recognition','adapt',1),(1,2,'meaning_recall','adopt',2);
 INSERT INTO answer_logs(user_id,session_id,question_id,request_id,raw_answer,result,response_time_ms) VALUES
 ('demo',1,1,'old-answer-0001','适应','correct',1200),('demo',1,2,'old-answer-0002','选择','pending',1000);
 INSERT INTO review_states(user_id,sense_id,dimension,attempts,correct_count,next_review_at) VALUES('demo',1,'recognition',2,2,'2026-10-12T08:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var attempts, correct int
	var due time.Time
	var card []byte
	if err = db.QueryRowContext(ctx, `SELECT attempts,correct_count,next_review_at,fsrs_card FROM review_states WHERE user_id='demo'`).Scan(&attempts, &correct, &due, &card); err != nil {
		t.Fatal(err)
	}
	expected, _ := time.Parse(time.RFC3339, "2026-10-12T08:00:00Z")
	if attempts != 2 || correct != 2 || !due.Equal(expected) || len(card) != 0 {
		t.Fatal("migration fabricated memory or lost historical state")
	}
	ss, err := (&server{db: db}).loadSession(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.Questions) != 2 || ss.Questions[0].Rating == nil || *ss.Questions[0].Rating != 3 || ss.Questions[1].Rating != nil || *ss.Questions[1].Result != "pending" {
		t.Fatal("old completed and pending answers not preserved")
	}
}
