package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"example.com/els/api/internal/learning"
	"github.com/jackc/pgx/v5/pgconn"
	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

type answerInput struct {
	QuestionID   int64  `json:"question_id"`
	RequestID    string `json:"request_id"`
	Answer       string `json:"answer"`
	ResponseTime int    `json:"response_time_ms"`
	UsedHint     bool   `json:"used_hint"`
}
type reviewInput struct {
	QuestionID     int64  `json:"question_id"`
	RequestID      string `json:"request_id"`
	Rating         int    `json:"rating"`
	ResolvedResult string `json:"resolved_result"`
}
type reviewSnapshot struct {
	Attempts int        `json:"attempts"`
	Correct  int        `json:"correct_count"`
	Next     time.Time  `json:"next_review_at"`
	Card     *fsrs.Card `json:"card"`
}

func isCurrent(ctx context.Context, tx *sql.Tx, session, question int64) (bool, error) {
	var current int64
	err := tx.QueryRowContext(ctx, `SELECT q.id FROM study_questions q JOIN study_sessions ss ON ss.id=q.session_id
 LEFT JOIN answer_logs a ON a.question_id=q.id AND a.undone_at IS NULL
 WHERE q.session_id=$1 AND ss.user_id=$2 AND a.rating IS NULL ORDER BY q.position LIMIT 1`, session, userID(ctx)).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return current == question, err
}

func (s *server) answer(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var input answerInput
	if err := decode(r, &input); err != nil || input.QuestionID < 1 || !requestIDPattern.MatchString(input.RequestID) || len([]rune(input.Answer)) > 200 || input.ResponseTime < 0 || input.ResponseTime > 3600000 {
		fail(w, 400, "答案格式无效")
		return
	}
	ctx := r.Context()
	tx, err := s.studyTx(ctx)
	if err != nil {
		s.dbError(w, "submit answer", err)
		return
	}
	defer tx.Rollback()
	var senseID int64
	var kind, lemma, meaning string
	err = tx.QueryRowContext(ctx, `SELECT q.sense_id,q.question_type,w.lemma,ws.meaning_zh
 FROM study_questions q JOIN study_sessions ss ON ss.id=q.session_id JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id
 WHERE q.id=$1 AND ss.id=$2 AND ss.user_id=$3 FOR UPDATE OF q`, input.QuestionID, id, userID(ctx)).Scan(&senseID, &kind, &lemma, &meaning)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "题目不存在")
		return
	}
	if err != nil {
		s.dbError(w, "load answer question", err)
		return
	}
	var old answerInput
	var result string
	err = tx.QueryRowContext(ctx, `SELECT request_id,raw_answer,initial_result,response_time_ms,used_hint FROM answer_logs WHERE question_id=$1 AND undone_at IS NULL`, input.QuestionID).Scan(&old.RequestID, &old.Answer, &result, &old.ResponseTime, &old.UsedHint)
	if err == nil {
		if old.RequestID != input.RequestID || old.Answer != input.Answer || old.ResponseTime != input.ResponseTime || old.UsedHint != input.UsedHint {
			fail(w, 409, "本题已经提交，请恢复当前进度")
			return
		}
		respond(w, 200, map[string]any{"result": result, "expected": meaningOrLemma(kind, meaning, lemma), "used_hint": old.UsedHint, "replayed": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "answer replay", err)
		return
	}
	current, err := isCurrent(ctx, tx, id, input.QuestionID)
	if err != nil {
		s.dbError(w, "current answer", err)
		return
	}
	if !current {
		fail(w, 409, "请先完成当前题目")
		return
	}
	rows, err := tx.QueryContext(ctx, `SELECT normalized_zh FROM sense_aliases WHERE sense_id=$1`, senseID)
	if err != nil {
		s.dbError(w, "answer aliases", err)
		return
	}
	aliases := []string{}
	for rows.Next() {
		var alias string
		if err = rows.Scan(&alias); err != nil {
			break
		}
		aliases = append(aliases, alias)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "read aliases", err)
		return
	}
	result = learning.Grade(kind, input.Answer, lemma, meaning, aliases)
	_, err = tx.ExecContext(ctx, `INSERT INTO answer_logs(user_id,session_id,question_id,request_id,raw_answer,result,initial_result,response_time_ms,used_hint) VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8)`, userID(ctx), id, input.QuestionID, input.RequestID, input.Answer, result, input.ResponseTime, input.UsedHint)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			fail(w, 409, "请求编号已使用")
			return
		}
		s.dbError(w, "insert answer", err)
		return
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "commit answer", err)
		return
	}
	respond(w, 200, map[string]any{"result": result, "expected": meaningOrLemma(kind, meaning, lemma), "used_hint": input.UsedHint})
}

func (s *server) review(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var input reviewInput
	if err := decode(r, &input); err != nil || input.QuestionID < 1 || !requestIDPattern.MatchString(input.RequestID) || input.Rating < 1 || input.Rating > 4 {
		fail(w, 400, "记忆评价无效")
		return
	}
	ctx := r.Context()
	tx, err := s.studyTx(ctx)
	if err != nil {
		s.dbError(w, "review", err)
		return
	}
	defer tx.Rollback()
	var logID, senseID int64
	var kind, mode, result, initial string
	var usedHint bool
	var rating sql.NullInt32
	var oldRequest, oldResolution sql.NullString
	var oldNext sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT a.id,q.sense_id,q.question_type,ss.mode,a.result,a.initial_result,a.used_hint,a.rating,a.review_request_id,a.resolved_result,a.next_review_at
 FROM answer_logs a JOIN study_questions q ON q.id=a.question_id JOIN study_sessions ss ON ss.id=a.session_id
 WHERE a.question_id=$1 AND ss.id=$2 AND ss.user_id=$3 AND a.undone_at IS NULL FOR UPDATE OF a`, input.QuestionID, id, userID(ctx)).Scan(&logID, &senseID, &kind, &mode, &result, &initial, &usedHint, &rating, &oldRequest, &oldResolution, &oldNext)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "请先提交答案")
		return
	}
	if err != nil {
		s.dbError(w, "review answer", err)
		return
	}
	if rating.Valid {
		if oldRequest.String != input.RequestID || int(rating.Int32) != input.Rating || oldResolution.String != input.ResolvedResult {
			fail(w, 409, "本题已完成记忆评价")
			return
		}
		var next *time.Time
		if oldNext.Valid {
			next = &oldNext.Time
		}
		respond(w, 200, map[string]any{"result": result, "rating": input.Rating, "next_review_at": next, "schedule_applied": mode == "scheduled", "replayed": true})
		return
	}
	current, err := isCurrent(ctx, tx, id, input.QuestionID)
	if err != nil {
		s.dbError(w, "current review", err)
		return
	}
	if !current {
		fail(w, 409, "请先完成当前题目")
		return
	}
	if initial == "pending" {
		if input.ResolvedResult != "correct" && input.ResolvedResult != "wrong" {
			fail(w, 400, "请对照参考答案确认是否答对")
			return
		}
		result = input.ResolvedResult
	} else if input.ResolvedResult != "" {
		fail(w, 400, "自动判分答案不能改为自评")
		return
	}
	var previous *reviewSnapshot
	if mode == "scheduled" {
		v := reviewSnapshot{}
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT attempts,correct_count,next_review_at,fsrs_card FROM review_states WHERE user_id=$1 AND sense_id=$2 AND dimension=$3 FOR UPDATE`, userID(ctx), senseID, kind).Scan(&v.Attempts, &v.Correct, &v.Next, &raw)
		if err == nil {
			if len(raw) > 0 {
				if err = json.Unmarshal(raw, &v.Card); err != nil {
					s.dbError(w, "decode FSRS card", err)
					return
				}
			}
			previous = &v
		} else if !errors.Is(err, sql.ErrNoRows) {
			s.dbError(w, "review state", err)
			return
		}
	}
	var card *fsrs.Card
	if previous != nil {
		card = previous.Card
	}
	prefs, err := readPreferences(ctx, tx)
	if err != nil {
		s.dbError(w, "review preferences", err)
		return
	}
	scheduled, err := learning.Schedule(card, time.Now(), result, usedHint, input.Rating, prefs.Retention)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	var next any
	var fsrsLog any
	var snapshot any
	if mode == "scheduled" {
		raw, _ := json.Marshal(scheduled.Card)
		logJSON, _ := json.Marshal(scheduled.ReviewLog)
		previousJSON, _ := json.Marshal(previous)
		next, fsrsLog, snapshot = scheduled.Card.Due, string(logJSON), string(previousJSON)
		_, err = tx.ExecContext(ctx, `INSERT INTO review_states(user_id,sense_id,dimension,attempts,correct_count,next_review_at,fsrs_card) VALUES($1,$2,$3,1,$4,$5,$6)
  ON CONFLICT(user_id,sense_id,dimension) DO UPDATE SET attempts=review_states.attempts+1,correct_count=review_states.correct_count+$4,next_review_at=$5,fsrs_card=$6`, userID(ctx), senseID, kind, boolInt(result == "correct" && !usedHint && input.Rating > 1), next, string(raw))
		if err != nil {
			s.dbError(w, "save FSRS card", err)
			return
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE answer_logs SET result=$2,rating=$3,reviewed_at=now(),schedule_applied=$4,previous_review=$5,fsrs_log=$6,next_review_at=$7,review_request_id=$8,resolved_result=$9 WHERE id=$1`, logID, result, input.Rating, mode == "scheduled", snapshot, fsrsLog, next, input.RequestID, input.ResolvedResult)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			fail(w, 409, "评价请求编号已使用")
			return
		}
		s.dbError(w, "save review log", err)
		return
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "commit review", err)
		return
	}
	respond(w, 200, map[string]any{"result": result, "rating": input.Rating, "next_review_at": next, "schedule_applied": mode == "scheduled"})
}

func (s *server) undo(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	var input struct {
		RequestID string `json:"request_id"`
	}
	if err := decode(r, &input); err != nil || !requestIDPattern.MatchString(input.RequestID) {
		fail(w, 400, "撤销请求无效")
		return
	}
	ctx := r.Context()
	tx, err := s.studyTx(ctx)
	if err != nil {
		s.dbError(w, "undo", err)
		return
	}
	defer tx.Rollback()
	var previousSession, previousQuestion int64
	err = tx.QueryRowContext(ctx, `SELECT session_id,question_id FROM undo_requests WHERE user_id=$1 AND request_id=$2`, userID(ctx), input.RequestID).Scan(&previousSession, &previousQuestion)
	if err == nil {
		if previousSession != id {
			fail(w, 409, "撤销请求编号已使用")
			return
		}
		respond(w, 200, map[string]any{"question_id": previousQuestion, "replayed": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "undo replay", err)
		return
	}
	if active, err := findActive(ctx, tx); err == nil && active != id {
		fail(w, 409, "请先完成当前练习")
		return
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "undo active", err)
		return
	}
	var logID, logSession, questionID, senseID int64
	var kind string
	var snapshot []byte
	var applied bool
	var reviewed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT a.id,a.session_id,a.question_id,q.sense_id,q.question_type,a.previous_review,a.schedule_applied,a.reviewed_at
 FROM answer_logs a JOIN study_questions q ON q.id=a.question_id WHERE a.user_id=$1 AND a.undone_at IS NULL ORDER BY a.id DESC LIMIT 1 FOR UPDATE OF a`, userID(ctx)).Scan(&logID, &logSession, &questionID, &senseID, &kind, &snapshot, &applied, &reviewed)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 409, "没有可撤销的题目")
		return
	}
	if err != nil {
		s.dbError(w, "undo latest", err)
		return
	}
	if logSession != id {
		fail(w, 409, "只能撤销最近练习中的题目")
		return
	}
	// Phase 0 reviews have no complete snapshot and cannot be safely restored.
	if applied && len(snapshot) == 0 {
		fail(w, 409, "旧版记录没有完整快照，无法撤销")
		return
	}
	if applied {
		var previous *reviewSnapshot
		if err = json.Unmarshal(snapshot, &previous); err != nil {
			s.dbError(w, "decode undo snapshot", err)
			return
		}
		if previous == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM review_states WHERE user_id=$1 AND sense_id=$2 AND dimension=$3`, userID(ctx), senseID, kind)
		} else {
			var raw any
			if previous.Card != nil {
				v, _ := json.Marshal(previous.Card)
				raw = string(v)
			}
			_, err = tx.ExecContext(ctx, `UPDATE review_states SET attempts=$4,correct_count=$5,next_review_at=$6,fsrs_card=$7 WHERE user_id=$1 AND sense_id=$2 AND dimension=$3`, userID(ctx), senseID, kind, previous.Attempts, previous.Correct, previous.Next, raw)
		}
		if err != nil {
			s.dbError(w, "restore review", err)
			return
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE answer_logs SET undone_at=now() WHERE id=$1`, logID); err != nil {
		s.dbError(w, "undo answer", err)
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO undo_requests(user_id,request_id,session_id,question_id) VALUES($1,$2,$3,$4)`, userID(ctx), input.RequestID, id, questionID); err != nil {
		s.dbError(w, "undo request", err)
		return
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "commit undo", err)
		return
	}
	respond(w, 200, map[string]any{"question_id": questionID})
}
