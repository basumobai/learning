package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"example.com/els/api/internal/learning"
	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"
)

type question struct {
	ID         int64             `json:"id"`
	Type       string            `json:"type"`
	Prompt     string            `json:"prompt"`
	HintText   string            `json:"hint_text"`
	Choices    []string          `json:"choices,omitempty"`
	Position   int               `json:"position"`
	Result     *string           `json:"result"`
	Rating     *int              `json:"rating"`
	Answer     string            `json:"answer,omitempty"`
	Expected   string            `json:"expected,omitempty"`
	Example    string            `json:"example,omitempty"`
	UsedHint   bool              `json:"used_hint"`
	Intervals  map[int]time.Time `json:"intervals,omitempty"`
	Phonetic   string            `json:"phonetic,omitempty"`
	POS        string            `json:"pos,omitempty"`
	NextReview *time.Time        `json:"next_review_at,omitempty"`
}

type studySession struct {
	ID        int64      `json:"id"`
	Mode      string     `json:"mode"`
	Questions []question `json:"questions"`
}

func sessionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "练习编号无效")
		return 0, false
	}
	return id, true
}

// Serializing one authenticated user's mutations keeps sessions, snapshots and
// undo safe across tabs without locking other users.
func (s *server) studyTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var user string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID(ctx)).Scan(&user); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func findActive(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT ss.id FROM study_sessions ss WHERE ss.user_id=$1 AND EXISTS (
 SELECT 1 FROM study_questions q LEFT JOIN answer_logs a ON a.question_id=q.id AND a.undone_at IS NULL
 WHERE q.session_id=ss.id AND a.rating IS NULL) ORDER BY ss.id DESC LIMIT 1`, userID(ctx)).Scan(&id)
	return id, err
}

const siblingFilter = `NOT EXISTS (
 SELECT 1 FROM answer_logs a JOIN study_questions q ON q.id=a.question_id
 WHERE a.user_id=$1 AND a.undone_at IS NULL AND a.schedule_applied
 AND a.reviewed_at >= (date_trunc('day', now() AT TIME ZONE $4) AT TIME ZONE $4)
 AND q.sense_id=s.id AND q.question_type<>k.kind)`

func (s *server) createSession(w http.ResponseWriter, r *http.Request) {
	var options learning.Options
	if err := decode(r, &options); err != nil {
		fail(w, 400, "练习设置无效")
		return
	}
	if err := options.Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	for _, kind := range options.Types {
		if kind == "listening_spelling" {
			var allowed bool
			err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_models m JOIN ai_providers p ON p.id=m.provider_id JOIN group_models gm ON gm.model_id=m.id JOIN users u ON u.group_id=gm.group_id WHERE u.id=$1 AND m.kind='tts' AND m.enabled AND p.enabled)`, userID(ctx)).Scan(&allowed)
			if err != nil {
				s.dbError(w, "dictation permission", err)
				return
			}
			if !allowed {
				fail(w, 403, "听音拼写需要先授权一个自然语音模型")
				return
			}
		}
	}
	tx, err := s.studyTx(ctx)
	if err != nil {
		s.dbError(w, "create session", err)
		return
	}
	defer tx.Rollback()
	if id, err := findActive(ctx, tx); err == nil {
		respond(w, 200, map[string]any{"id": id, "resumed": true})
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "find active", err)
		return
	}
	prefs, err := readPreferences(ctx, tx)
	if err != nil {
		s.dbError(w, "queue preferences", err)
		return
	}
	introduced, err := newStudiedToday(ctx, tx, prefs.Timezone)
	if err != nil {
		s.dbError(w, "daily budget", err)
		return
	}
	if options.Mode == "scheduled" {
		remaining := max(0, prefs.DailyNew-introduced)
		n := min(*options.NewLimit, remaining)
		options.NewLimit = &n
	}
	typeJSON, _ := json.Marshal(options.Types)
	rows, err := tx.QueryContext(ctx, `SELECT s.id,w.lemma,s.meaning_zh,s.part_of_speech,k.kind,rs.next_review_at
 FROM word_senses s JOIN words w ON w.id=s.word_id
 CROSS JOIN jsonb_array_elements_text($2::jsonb) AS k(kind)
 LEFT JOIN review_states rs ON rs.user_id=$1 AND rs.sense_id=s.id AND rs.dimension=k.kind
 WHERE NOT EXISTS(SELECT 1 FROM suspended_senses x WHERE x.user_id=$1 AND x.sense_id=s.id) AND ($3='practice' OR ((rs.next_review_at IS NULL OR rs.next_review_at<=now()) AND `+siblingFilter+`))
 ORDER BY rs.next_review_at ASC NULLS LAST,random() LIMIT 3000`, userID(ctx), string(typeJSON), options.Mode, prefs.Timezone)
	if err != nil {
		s.dbError(w, "queue candidates", err)
		return
	}
	candidates := []learning.Candidate{}
	for rows.Next() {
		var c learning.Candidate
		var due sql.NullTime
		if err = rows.Scan(&c.SenseID, &c.Lemma, &c.Meaning, &c.POS, &c.Kind, &due); err != nil {
			break
		}
		if due.Valid {
			c.Due = &due.Time
		}
		candidates = append(candidates, c)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "read queue", err)
		return
	}
	queue := learning.BuildQueue(candidates, options, time.Now(), rand.Shuffle)
	if len(queue) == 0 {
		respond(w, 200, map[string]any{"id": nil, "question_count": 0, "message": "当前没有符合条件的题目，可以稍后复习或选择自由练习。"})
		return
	}
	poolRows, err := tx.QueryContext(ctx, `SELECT meaning_zh FROM (SELECT DISTINCT meaning_zh FROM word_senses) AS pool ORDER BY random() LIMIT 100`)
	if err != nil {
		s.dbError(w, "choice pool", err)
		return
	}
	pool := []string{}
	for poolRows.Next() {
		var meaning string
		if err = poolRows.Scan(&meaning); err != nil {
			break
		}
		pool = append(pool, meaning)
	}
	if poolRows.Err() != nil {
		err = poolRows.Err()
	}
	poolRows.Close()
	if err != nil {
		s.dbError(w, "choice pool", err)
		return
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO study_sessions(user_id,mode) VALUES($1,$2) RETURNING id`, userID(ctx), options.Mode).Scan(&id); err != nil {
		s.dbError(w, "insert session", err)
		return
	}
	for i, v := range queue {
		prompt, hint := v.Lemma, "词性："+v.POS
		var choices any
		if v.Kind == "spelling" || v.Kind == "listening_spelling" {
			prompt = v.Meaning
			hint = string([]rune(v.Lemma)[:1]) + "…"
		}
		if v.Kind == "listening_spelling" {
			prompt = "听音填写单词"
		}
		if v.Kind == "recognition" {
			encoded, _ := json.Marshal(learning.Choices(v.Meaning, append([]string(nil), pool...)))
			choices = string(encoded)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO study_questions(session_id,sense_id,question_type,prompt,hint_text,choices,position) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, v.SenseID, v.Kind, prompt, hint, choices, i+1); err != nil {
			s.dbError(w, "insert question", err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "commit session", err)
		return
	}
	respond(w, 201, map[string]any{"id": id, "question_count": len(queue), "mode": options.Mode})
}

func (s *server) loadSession(ctx context.Context, id int64) (*studySession, error) {
	prefs, err := readPreferences(ctx, s.db)
	if err != nil {
		return nil, err
	}
	session := &studySession{ID: id, Questions: []question{}}
	err = s.db.QueryRowContext(ctx, `SELECT mode FROM study_sessions WHERE id=$1 AND user_id=$2`, id, userID(ctx)).Scan(&session.Mode)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,q.question_type,q.prompt,q.hint_text,q.choices,q.position,a.result,a.rating,
 COALESCE(a.raw_answer,''),COALESCE(a.used_hint,false),w.lemma,ws.meaning_zh,ws.example_en,rs.fsrs_card,a.next_review_at,ws.phonetic,ws.part_of_speech
 FROM study_questions q JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id
 LEFT JOIN answer_logs a ON a.question_id=q.id AND a.undone_at IS NULL
 LEFT JOIN review_states rs ON rs.user_id=$2 AND rs.sense_id=q.sense_id AND rs.dimension=q.question_type
 WHERE q.session_id=$1 ORDER BY q.position`, id, userID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var q question
		var raw, cardJSON []byte
		var result sql.NullString
		var rating sql.NullInt32
		var lemma, meaning, example, phonetic, pos string
		var next sql.NullTime
		if err = rows.Scan(&q.ID, &q.Type, &q.Prompt, &q.HintText, &raw, &q.Position, &result, &rating, &q.Answer, &q.UsedHint, &lemma, &meaning, &example, &cardJSON, &next, &phonetic, &pos); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &q.Choices); err != nil {
				return nil, err
			}
		}
		if result.Valid {
			q.Result = &result.String
			q.Expected = meaningOrLemma(q.Type, meaning, lemma)
			q.Example = example
			q.Phonetic = phonetic
			q.POS = pos
		}
		if rating.Valid {
			n := int(rating.Int32)
			q.Rating = &n
		}
		if next.Valid {
			q.NextReview = &next.Time
		}
		if q.Result != nil && q.Rating == nil && session.Mode == "scheduled" {
			var card *fsrs.Card
			if len(cardJSON) > 0 {
				if err = json.Unmarshal(cardJSON, &card); err != nil {
					return nil, err
				}
			}
			if q.Intervals, err = learning.Preview(card, time.Now(), prefs.Retention); err != nil {
				return nil, err
			}
		}
		session.Questions = append(session.Questions, q)
	}
	return session, rows.Err()
}

func (s *server) getSession(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	session, err := s.loadSession(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "练习不存在")
		return
	}
	if err != nil {
		s.dbError(w, "load session", err)
		return
	}
	respond(w, 200, session)
}

func (s *server) activeSession(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		s.dbError(w, "active session", err)
		return
	}
	defer tx.Rollback()
	id, err := findActive(r.Context(), tx)
	if errors.Is(err, sql.ErrNoRows) {
		respond(w, 200, map[string]any{"session": nil})
		return
	}
	if err != nil {
		s.dbError(w, "active session", err)
		return
	}
	session, err := s.loadSession(r.Context(), id)
	if err != nil {
		s.dbError(w, "active session", err)
		return
	}
	respond(w, 200, map[string]any{"session": session})
}

func meaningOrLemma(kind, meaning, lemma string) string {
	if kind == "spelling" || kind == "listening_spelling" {
		return lemma
	}
	return meaning
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
