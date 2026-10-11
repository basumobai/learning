package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type preferences struct {
	DailyNew  int     `json:"daily_new"`
	Retention float64 `json:"retention"`
	Timezone  string  `json:"timezone"`
	TTSModel  *int64  `json:"tts_model_id"`
	Voice     string  `json:"voice"`
}
type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readPreferences(ctx context.Context, db queryRower) (preferences, error) {
	p := preferences{DailyNew: 10, Retention: .9, Timezone: "Asia/Shanghai", Voice: "cedar"}
	err := db.QueryRowContext(ctx, `SELECT daily_new,retention,timezone,tts_model_id,voice FROM user_preferences WHERE user_id=$1`, userID(ctx)).Scan(&p.DailyNew, &p.Retention, &p.Timezone, &p.TTSModel, &p.Voice)
	if err == sql.ErrNoRows {
		err = nil
	}
	return p, err
}
func (s *server) preferences(w http.ResponseWriter, r *http.Request) {
	p, err := readPreferences(r.Context(), s.db)
	if err != nil {
		s.dbError(w, "preferences", err)
		return
	}
	respond(w, 200, p)
}
func (s *server) savePreferences(w http.ResponseWriter, r *http.Request) {
	var p preferences
	if decode(r, &p) != nil || p.DailyNew < 0 || p.DailyNew > 100 || p.Retention < .8 || p.Retention > .97 || len(p.Timezone) > 100 || len(p.Voice) > 120 {
		fail(w, 400, "复习设置无效")
		return
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		fail(w, 400, "时区无效")
		return
	}
	if p.TTSModel != nil {
		model, err := s.allowedModel(r.Context(), *p.TTSModel, "tts")
		if err != nil {
			fail(w, 403, "语音模型未获授权")
			return
		}
		if !model.hasVoice(p.Voice) {
			fail(w, 400, "模型不支持此音色")
			return
		}
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "preferences transaction", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO user_preferences(user_id,daily_new,retention,timezone,tts_model_id,voice) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(user_id) DO UPDATE SET daily_new=$2,retention=$3,timezone=$4,tts_model_id=$5,voice=$6`, userID(r.Context()), p.DailyNew, p.Retention, p.Timezone, p.TTSModel, p.Voice)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save preferences", err)
		return
	}
	respond(w, 200, p)
}
func newStudiedToday(ctx context.Context, db queryRower, timezone string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT q.sense_id) FROM answer_logs a JOIN study_questions q ON q.id=a.question_id WHERE a.user_id=$1 AND a.undone_at IS NULL AND a.schedule_applied AND a.previous_review='null'::jsonb AND a.reviewed_at>=(date_trunc('day',now() AT TIME ZONE $2) AT TIME ZONE $2)`, userID(ctx), timezone).Scan(&n)
	return n, err
}

// Voice changes must not overwrite review settings edited in another page/tab.
func (s *server) saveVoice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ModelID int64  `json:"model_id"`
		Voice   string `json:"voice"`
	}
	if decode(r, &input) != nil {
		fail(w, 400, "语音设置无效")
		return
	}
	model, err := s.allowedModel(r.Context(), input.ModelID, "tts")
	if err != nil {
		fail(w, 403, "语音模型未获授权")
		return
	}
	if !model.hasVoice(input.Voice) {
		fail(w, 400, "模型不支持此音色")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "voice preference", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO user_preferences(user_id,tts_model_id,voice) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET tts_model_id=$2,voice=$3`, userID(r.Context()), input.ModelID, input.Voice)
	if err != nil {
		s.dbError(w, "voice preference save", err)
		return
	}
	prefs, err := readPreferences(r.Context(), tx)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "voice preference commit", err)
		return
	}
	respond(w, 200, prefs)
}
