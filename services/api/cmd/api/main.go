package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"example.com/els/api/internal/learning"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const demoUser = "demo"

type server struct { db *sql.DB }
type question struct {
	ID int64 `json:"id"`
	Type string `json:"type"`
	Prompt string `json:"prompt"`
	HintText string `json:"hint_text"`
	Choices []string `json:"choices,omitempty"`
	Position int `json:"position"`
	Result *string `json:"result"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" { log.Fatal("DATABASE_URL required") }
	db, err := sql.Open("pgx", dsn)
	if err != nil { log.Fatal(err) }
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 30; attempt++ {
		if err = db.PingContext(ctx); err == nil { break }
		time.Sleep(time.Second)
	}
	if err != nil { log.Fatal(err) }
	if err := migrate(ctx, db); err != nil { log.Fatal(err) }
	s := &server{db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	mux.HandleFunc("POST /api/v1/study/sessions", s.createSession)
	mux.HandleFunc("GET /api/v1/study/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/v1/study/sessions/{id}/answers", s.answer)
	addr := os.Getenv("ADDR")
	if addr == "" { addr = ":8080" }
	log.Printf("ELS API listening on %s", addr)
	log.Fatal((&http.Server{Addr:addr,Handler:mux,ReadHeaderTimeout:5*time.Second}).ListenAndServe())
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil { return err }
	paths, err := filepath.Glob("/app/migrations/*.sql")
	if err != nil { return err }
	if len(paths) == 0 { return fmt.Errorf("no migrations found") }
	for _, path := range paths {
		name := filepath.Base(path)
		tx, err := db.BeginTx(ctx,nil)
		if err != nil { return err }
		var exists bool
		err = tx.QueryRowContext(ctx,`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`,name).Scan(&exists)
		if err != nil { tx.Rollback(); return err }
		if exists { tx.Rollback(); continue }
		body, err := os.ReadFile(path)
		if err != nil { tx.Rollback(); return err }
		if _, err = tx.ExecContext(ctx,string(body)); err != nil { tx.Rollback(); return fmt.Errorf("%s: %w",name,err) }
		if _, err = tx.ExecContext(ctx,`INSERT INTO schema_migrations(version) VALUES($1)`,name); err != nil { tx.Rollback(); return err }
		if err = tx.Commit(); err != nil { return err }
	}
	return nil
}

func respond(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type","application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func fail(w http.ResponseWriter, status int, message string) { respond(w,status,map[string]string{"error":message}) }
func decode(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil,r.Body,4096)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil { return err }
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err,io.EOF) { if err == nil { return fmt.Errorf("extra JSON") }; return err }
	return nil
}

func (s *server) health(w http.ResponseWriter,r *http.Request) {
	ctx,cancel := context.WithTimeout(r.Context(),2*time.Second); defer cancel()
	if err := s.db.PingContext(ctx); err != nil { fail(w,503,"database unavailable"); return }
	respond(w,200,map[string]string{"status":"ok"})
}
func (s *server) dashboard(w http.ResponseWriter,r *http.Request) {
	var due,seen int
	err := s.db.QueryRowContext(r.Context(),`SELECT COUNT(*) FROM review_states WHERE user_id=$1 AND next_review_at<=now()`,demoUser).Scan(&due)
	if err == nil { err = s.db.QueryRowContext(r.Context(),`SELECT COUNT(DISTINCT sense_id) FROM review_states WHERE user_id=$1`,demoUser).Scan(&seen) }
	if err != nil { fail(w,500,"dashboard unavailable"); return }
	respond(w,200,map[string]any{"display_name":"体验用户","due":due,"studied":seen,"demo_words":5})
}

func (s *server) createSession(w http.ResponseWriter,r *http.Request) {
	ctx := r.Context()
	tx,err := s.db.BeginTx(ctx,nil)
	if err != nil { fail(w,500,"session unavailable"); return }
	defer tx.Rollback()
	var id int64
	if err=tx.QueryRowContext(ctx,`INSERT INTO study_sessions(user_id) VALUES($1) RETURNING id`,demoUser).Scan(&id); err!=nil { fail(w,500,"session unavailable"); return }
	rows,err := tx.QueryContext(ctx,`SELECT s.id,w.lemma,s.meaning_zh,s.part_of_speech FROM word_senses s JOIN words w ON w.id=s.word_id ORDER BY s.id LIMIT 5`)
	if err!=nil { fail(w,500,"word list unavailable"); return }
	type entry struct { id int64; lemma,meaning,pos string }
	var words []entry
	for rows.Next() { var v entry; if err=rows.Scan(&v.id,&v.lemma,&v.meaning,&v.pos); err!=nil { break }; words=append(words,v) }
	if rows.Err()!=nil { err=rows.Err() }
	rows.Close()
	if err!=nil || len(words)<4 { fail(w,500,"sample words unavailable"); return }
	position:=0
	for _,kind := range []string{"recognition","meaning_recall","spelling"} {
		for i,v := range words {
			position++
			prompt:=v.lemma
			hintText:=""
			var choices any
			if kind=="spelling" { prompt=v.meaning; hintText=string([]rune(v.lemma)[:1])+"…" }
			if kind=="meaning_recall" { hintText="词性："+v.pos }
			if kind=="recognition" {
				options:=[]string{v.meaning}
				for step:=1;step<4;step++ { options=append(options,words[(i+step)%len(words)].meaning) }
				options=append(options[1:i%4+1],append([]string{options[0]},options[i%4+1:]...)...)
				encoded,_:=json.Marshal(options)
				choices=string(encoded)
			}
			_,err=tx.ExecContext(ctx,`INSERT INTO study_questions(session_id,sense_id,question_type,prompt,hint_text,choices,position) VALUES($1,$2,$3,$4,$5,$6,$7)`,id,v.id,kind,prompt,hintText,choices,position)
			if err!=nil { fail(w,500,"question creation failed"); return }
		}
	}
	if err=tx.Commit(); err!=nil { fail(w,500,"session unavailable"); return }
	respond(w,201,map[string]any{"id":id,"question_count":position})
}

func (s *server) getSession(w http.ResponseWriter,r *http.Request) {
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil || id<1 { fail(w,400,"invalid session id"); return }
	rows,err:=s.db.QueryContext(r.Context(),`SELECT q.id,q.question_type,q.prompt,q.hint_text,q.choices,q.position,a.result FROM study_questions q JOIN study_sessions s ON s.id=q.session_id LEFT JOIN answer_logs a ON a.question_id=q.id WHERE q.session_id=$1 AND s.user_id=$2 ORDER BY q.position`,id,demoUser)
	if err!=nil { fail(w,500,"session unavailable"); return }
	defer rows.Close()
	items:=[]question{}
	for rows.Next() {
		var q question; var raw []byte; var result sql.NullString
		if err=rows.Scan(&q.ID,&q.Type,&q.Prompt,&q.HintText,&raw,&q.Position,&result); err!=nil { fail(w,500,"session unavailable"); return }
		if len(raw)>0 { if err=json.Unmarshal(raw,&q.Choices); err!=nil { fail(w,500,"invalid question data"); return } }
		if result.Valid { q.Result=&result.String }
		items=append(items,q)
	}
	if rows.Err()!=nil { fail(w,500,"session unavailable"); return }
	if len(items)==0 { fail(w,404,"session not found"); return }
	respond(w,200,map[string]any{"id":id,"questions":items})
}

func (s *server) answer(w http.ResponseWriter,r *http.Request) {
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil || id<1 { fail(w,400,"invalid session id"); return }
	var input struct {
		QuestionID int64 `json:"question_id"`
		RequestID string `json:"request_id"`
		Answer string `json:"answer"`
		ResponseTime int `json:"response_time_ms"`
		UsedHint bool `json:"used_hint"`
	}
	if err=decode(r,&input);err!=nil || input.QuestionID<1 || len(input.RequestID)<8 || len(input.RequestID)>128 || len([]rune(input.Answer))>200 || input.ResponseTime<0 || input.ResponseTime>3600000 { fail(w,400,"invalid answer"); return }
	ctx:=r.Context()
	tx,err:=s.db.BeginTx(ctx,nil)
	if err!=nil { fail(w,500,"answer unavailable"); return }
	defer tx.Rollback()
	var senseID int64; var kind,lemma,meaning string
	err=tx.QueryRowContext(ctx,`SELECT q.sense_id,q.question_type,w.lemma,s.meaning_zh FROM study_questions q JOIN study_sessions ss ON ss.id=q.session_id JOIN word_senses s ON s.id=q.sense_id JOIN words w ON w.id=s.word_id WHERE q.id=$1 AND ss.id=$2 AND ss.user_id=$3 FOR UPDATE OF q`,input.QuestionID,id,demoUser).Scan(&senseID,&kind,&lemma,&meaning)
	if errors.Is(err,sql.ErrNoRows) { fail(w,404,"question not found"); return }
	if err!=nil { fail(w,500,"answer unavailable"); return }
	var oldRequest,oldAnswer,oldResult string; var oldTime int; var oldHint bool
	err=tx.QueryRowContext(ctx,`SELECT request_id,raw_answer,result,response_time_ms,used_hint FROM answer_logs WHERE question_id=$1`,input.QuestionID).Scan(&oldRequest,&oldAnswer,&oldResult,&oldTime,&oldHint)
	if err==nil {
		if oldRequest!=input.RequestID || oldAnswer!=input.Answer || oldTime!=input.ResponseTime || oldHint!=input.UsedHint { fail(w,409,"question already answered"); return }
		response:=map[string]any{"result":oldResult,"replayed":true}
		if oldResult!="pending" { response["expected"]=meaningOrLemma(kind,meaning,lemma) }
		respond(w,200,response); return
	}
	if !errors.Is(err,sql.ErrNoRows) { fail(w,500,"answer unavailable"); return }
	rows,err:=tx.QueryContext(ctx,`SELECT normalized_zh FROM sense_aliases WHERE sense_id=$1`,senseID)
	if err!=nil { fail(w,500,"answer unavailable"); return }
	aliases:=[]string{}
	for rows.Next() { var a string; if err=rows.Scan(&a);err!=nil { break }; aliases=append(aliases,a) }
	if rows.Err()!=nil { err=rows.Err() }
	rows.Close()
	if err!=nil { fail(w,500,"answer unavailable"); return }
	result:=learning.Grade(kind,input.Answer,lemma,meaning,aliases)
	_,err=tx.ExecContext(ctx,`INSERT INTO answer_logs(user_id,session_id,question_id,request_id,raw_answer,result,response_time_ms,used_hint) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,demoUser,id,input.QuestionID,input.RequestID,input.Answer,result,input.ResponseTime,input.UsedHint)
	if err!=nil {
		if strings.Contains(err.Error(),"answer_logs_user_id_request_id_key") { fail(w,409,"request id already used"); return }
		fail(w,500,"answer unavailable");return
	}
	if result!="pending" {
		var attempts int
		err=tx.QueryRowContext(ctx,`SELECT attempts FROM review_states WHERE user_id=$1 AND sense_id=$2 AND dimension=$3 FOR UPDATE`,demoUser,senseID,kind).Scan(&attempts)
		if err!=nil && !errors.Is(err,sql.ErrNoRows) { fail(w,500,"review unavailable"); return }
		next:=learning.NextReview(time.Now(),result=="correct",input.UsedHint,attempts)
		_,err=tx.ExecContext(ctx,`INSERT INTO review_states(user_id,sense_id,dimension,attempts,correct_count,next_review_at) VALUES($1,$2,$3,1,$4,$5) ON CONFLICT(user_id,sense_id,dimension) DO UPDATE SET attempts=review_states.attempts+1,correct_count=review_states.correct_count+$4,next_review_at=$5`,demoUser,senseID,kind,boolInt(result=="correct" && !input.UsedHint),next)
		if err!=nil { fail(w,500,"review unavailable"); return }
	}
	if err=tx.Commit();err!=nil { fail(w,500,"answer unavailable");return }
	response:=map[string]any{"result":result,"used_hint":input.UsedHint}
	if result!="pending" { response["expected"]=meaningOrLemma(kind,meaning,lemma) }
	respond(w,200,response)
}
func meaningOrLemma(kind,meaning,lemma string) string { if kind=="spelling" { return lemma }; return meaning }
func boolInt(b bool) int { if b { return 1 }; return 0 }
