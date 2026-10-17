package main

import (
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/learning"
	"example.com/els/api/internal/security"
	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
	"net/http"
	"strings"
	"time"
)

func (s *server) reviewItemRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/learning/review-items", s.reviewItems)
	m.HandleFunc("POST /api/v1/learning/review-items", s.createReviewItem)
	m.HandleFunc("POST /api/v1/learning/review-items/{id}/reveal", s.revealReviewItem)
	m.HandleFunc("POST /api/v1/learning/review-items/{id}/rate", s.rateReviewItem)
	m.HandleFunc("POST /api/v1/learning/review-items/{id}/undo", s.undoReviewItem)
}
func (s *server) reviewItems(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT id,subject_id,front,due,version FROM learning_review_items WHERE user_id=$1 ORDER BY due,id LIMIT 200)t`, userID(r.Context())).Scan(&raw)
	if err != nil {
		s.dbError(w, "review items", err)
		return
	}
	respond(w, 200, map[string]any{"items": raw, "server_time": time.Now()})
}
func (s *server) createReviewItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Attempt string `json:"attempt_id"`
		Front   string `json:"front"`
		Back    string `json:"back"`
	}
	if decodeLimit(r, &in, 12000) != nil || strings.TrimSpace(in.Front) == "" || strings.TrimSpace(in.Back) == "" || len([]rune(in.Front)) > 1000 || len([]rune(in.Back)) > 2000 {
		fail(w, 400, "请填写简短的主动回忆问题与答案")
		return
	}
	a, err := s.loadAttempt(r.Context(), in.Attempt)
	if err != nil || a.Submitted == nil {
		fail(w, 403, "只能从本人已交卷练习创建回忆卡")
		return
	}
	id := security.Token(18)
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_review_items(id,user_id,subject_id,source_attempt_id,front,back) VALUES($1,$2,$3,$4,$5,$6)`, id, userID(r.Context()), a.Question.Subject, a.ID, in.Front, in.Back)
	if err != nil {
		s.dbError(w, "create recall item", err)
		return
	}
	respond(w, 201, map[string]string{"id": id, "source": "self_authored", "note": "自建主动回忆卡，不写入学科能力事实"})
}
func (s *server) revealReviewItem(w http.ResponseWriter, r *http.Request) {
	var back string
	var raw []byte
	var version int
	err := s.db.QueryRowContext(r.Context(), `SELECT back,fsrs_card,version FROM learning_review_items WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&back, &raw, &version)
	if err != nil {
		fail(w, 404, "回忆卡不存在")
		return
	}
	var card *fsrs.Card
	if len(raw) > 0 {
		if json.Unmarshal(raw, &card) != nil {
			fail(w, 409, "卡片状态无效")
			return
		}
	}
	preview, err := learning.Preview(card, time.Now())
	if err != nil {
		s.dbError(w, "FSRS preview", err)
		return
	}
	respond(w, 200, map[string]any{"back": back, "intervals": preview, "version": version, "source": "self_authored"})
}
func (s *server) rateReviewItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string `json:"request_id"`
		Version int    `json:"version"`
		Rating  int    `json:"rating"`
		Result  string `json:"result"`
		Hint    bool   `json:"used_hint"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "复习评价无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "rate recall", err)
		return
	}
	defer tx.Rollback()
	var cardRaw []byte
	var due time.Time
	var version int
	err = tx.QueryRowContext(r.Context(), `SELECT fsrs_card,due,version FROM learning_review_items WHERE id=$1 AND user_id=$2 FOR UPDATE`, r.PathValue("id"), userID(r.Context())).Scan(&cardRaw, &due, &version)
	if err != nil {
		fail(w, 404, "回忆卡不存在")
		return
	}
	digest := payloadDigest([]any{r.PathValue("id"), in})
	var old, id string
	var next []byte
	err = tx.QueryRowContext(r.Context(), `SELECT id,digest,next_card FROM learning_review_events WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &old, &next)
	if err == nil {
		if old != digest {
			fail(w, 409, "同键复习内容冲突")
			return
		}
		respond(w, 200, map[string]any{"event_id": id, "card": json.RawMessage(next), "replayed": true})
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "review replay", err)
		return
	}
	if version != in.Version {
		fail(w, 409, "卡片已复习，请刷新")
		return
	}
	var card *fsrs.Card
	if len(cardRaw) > 0 {
		if json.Unmarshal(cardRaw, &card) != nil {
			fail(w, 409, "卡片状态无效")
			return
		}
	}
	result, err := learning.Schedule(card, time.Now(), in.Result, in.Hint, in.Rating)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	id = security.Token(18)
	var previous any
	if len(cardRaw) > 0 {
		previous = string(cardRaw)
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_review_events(id,item_id,user_id,request_id,digest,previous_card,previous_due,next_card,fsrs_log,rating,result,used_hint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, r.PathValue("id"), userID(r.Context()), in.Request, digest, previous, due, nullableJSON(result.Card), nullableJSON(result.ReviewLog), in.Rating, in.Result, in.Hint)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_review_items SET fsrs_card=$3,due=$4,version=version+1 WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context()), nullableJSON(result.Card), result.Card.Due)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "persist recall", err)
		return
	}
	respond(w, 200, map[string]any{"event_id": id, "card": result.Card})
}
func (s *server) undoReviewItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string `json:"request_id"`
		Event   string `json:"event_id"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) {
		fail(w, 400, "撤销请求无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "undo recall", err)
		return
	}
	defer tx.Rollback()
	var prior string
	err = tx.QueryRowContext(r.Context(), `SELECT event_id FROM learning_review_undos WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&prior)
	if err == nil {
		if prior != in.Event {
			fail(w, 409, "撤销请求冲突")
			return
		}
		respond(w, 200, map[string]bool{"undone": true})
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "undo replay", err)
		return
	}
	var id string
	var card []byte
	var due time.Time
	err = tx.QueryRowContext(r.Context(), `SELECT e.id,e.previous_card,e.previous_due FROM learning_review_events e WHERE e.item_id=$1 AND e.user_id=$2 AND NOT EXISTS(SELECT 1 FROM learning_review_undos u WHERE u.event_id=e.id) ORDER BY e.created_at DESC,e.id DESC LIMIT 1`, r.PathValue("id"), userID(r.Context())).Scan(&id, &card, &due)
	if err != nil || id != in.Event {
		fail(w, 409, "只可撤销该卡最后一条有效复习")
		return
	}
	var previous any
	if len(card) > 0 {
		previous = string(card)
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_review_undos(event_id,user_id,request_id) VALUES($1,$2,$3)`, id, userID(r.Context()), in.Request)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_review_items SET fsrs_card=$3,due=$4,version=version+1 WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context()), previous, due)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "undo recall commit", err)
		return
	}
	respond(w, 200, map[string]bool{"undone": true})
}
