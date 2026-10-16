package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"example.com/els/api/internal/security"
)

type speechInput struct {
	ModelID       int64  `json:"model_id"`
	Voice         string `json:"voice"`
	Input         string `json:"input"`
	Segment       string `json:"segment,omitempty"`
	pronunciation string
}

func (s *server) synthesize(ctx context.Context, input speechInput) ([]byte, error) {
	m, err := s.allowedModel(ctx, input.ModelID, "tts")
	if err != nil {
		return nil, fmt.Errorf("unauthorized speech model")
	}
	if !m.hasVoice(input.Voice) {
		return nil, fmt.Errorf("unauthorized voice")
	}
	if strings.TrimSpace(input.Input) == "" || len([]rune(input.Input)) > 4000 {
		return nil, fmt.Errorf("invalid speech input")
	}
	instructions := m.instructions
	if instructions != "" && input.pronunciation != "" {
		instructions += "\nPronunciation context (do not read aloud): " + input.pronunciation
	}
	raw, _ := json.Marshal([]any{m.ID, m.ModelID, m.version, instructions, input.Voice, input.Input})
	key := security.Digest(string(raw))
	var cached []byte
	err = s.db.QueryRowContext(ctx, `SELECT audio FROM speech_cache WHERE user_id=$1 AND cache_key=$2 AND created_at>now()-interval '30 days'`, userID(ctx), key).Scan(&cached)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	body := map[string]any{"model": m.ModelID, "voice": input.Voice, "input": input.Input, "response_format": "mp3"}
	if instructions != "" {
		body["instructions"] = instructions
	}
	audio, err := s.upstream(ctx, m, "/audio/speech", body, 8*1024*1024)
	if err != nil {
		return nil, err
	}
	if len(audio) < 3 || !(string(audio[:3]) == "ID3" || (audio[0] == 0xff && audio[1]&0xe0 == 0xe0)) {
		return nil, fmt.Errorf("provider returned invalid MP3")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO speech_cache(user_id,cache_key,audio) VALUES($1,$2,$3) ON CONFLICT(user_id,cache_key) DO UPDATE SET audio=$3,created_at=now()`, userID(ctx), key, audio)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM speech_cache WHERE user_id=$1 AND created_at<now()-interval '30 days'`, userID(ctx))
	return audio, nil
}
func (s *server) speechResponse(w http.ResponseWriter, r *http.Request, input speechInput) {
	m, err := s.allowedModel(r.Context(), input.ModelID, "tts")
	if err != nil {
		fail(w, 403, "当前权限组未授权这个语音模型，或服务尚未配置")
		return
	}
	if !m.hasVoice(input.Voice) {
		fail(w, 400, "语音模型不支持此音色")
		return
	}
	if strings.TrimSpace(input.Input) == "" || len([]rune(input.Input)) > 4000 {
		fail(w, 400, "语音文本应为 1–4000 字符")
		return
	}
	audio, err := s.synthesize(r.Context(), input)
	if err != nil {
		providerFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	w.WriteHeader(200)
	_, _ = w.Write(audio)
}
func (s *server) speech(w http.ResponseWriter, r *http.Request) {
	var input speechInput
	if decodeLimit(r, &input, 32768) != nil {
		fail(w, 400, "语音输入无效")
		return
	}
	s.speechResponse(w, r, input)
}
func (s *server) questionSpeech(w http.ResponseWriter, r *http.Request) {
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	qid, err := strconv.ParseInt(r.PathValue("question"), 10, 64)
	if err != nil {
		fail(w, 400, "题目编号无效")
		return
	}
	var input speechInput
	if decode(r, &input) != nil || input.Input != "" {
		fail(w, 400, "语音设置无效")
		return
	}
	var kind, word, phonetic, pos, meaning string
	var answered bool
	err = s.db.QueryRowContext(r.Context(), `SELECT q.question_type,w.lemma,EXISTS(SELECT 1 FROM answer_logs a WHERE a.question_id=q.id AND a.undone_at IS NULL),ws.phonetic,ws.part_of_speech,ws.meaning_zh FROM study_questions q JOIN study_sessions ss ON ss.id=q.session_id JOIN word_senses ws ON ws.id=q.sense_id JOIN words w ON w.id=ws.word_id WHERE ss.id=$1 AND ss.user_id=$2 AND q.id=$3`, id, userID(r.Context()), qid).Scan(&kind, &word, &answered, &phonetic, &pos, &meaning)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "题目不存在")
		return
	}
	if err != nil {
		s.dbError(w, "question audio", err)
		return
	}
	if !answered {
		tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			s.dbError(w, "audio current", err)
			return
		}
		current, err := isCurrent(r.Context(), tx, id, qid)
		tx.Rollback()
		if err != nil {
			s.dbError(w, "audio current", err)
			return
		}
		if !current {
			fail(w, 409, "请先完成当前题目")
			return
		}
		if kind == "spelling" {
			fail(w, 403, "中译英拼写交卷后才可听答案；听音拼写请选对应题型")
			return
		}
	}
	input.Input = word
	input.pronunciation = "IPA: " + phonetic + "; part of speech: " + pos + "; meaning: " + meaning
	s.speechResponse(w, r, input)
}
func (s *server) trainingSpeech(w http.ResponseWriter, r *http.Request) {
	var input speechInput
	if decode(r, &input) != nil || input.Input != "" {
		fail(w, 400, "语音设置无效")
		return
	}
	var raw []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT payload FROM training_sessions WHERE id=$1 AND user_id=$2 AND kind='listening'`, r.PathValue("id"), userID(r.Context())).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "听力练习不存在")
		return
	}
	if err != nil {
		s.dbError(w, "training audio", err)
		return
	}
	var lesson listeningLesson
	if err = json.Unmarshal(raw, &lesson); err != nil {
		s.dbError(w, "training transcript", err)
		return
	}
	input.Input, err = speakingSegment(lesson, input.Segment)
	if err != nil {
		fail(w, 400, "听力音频片段无效")
		return
	}
	s.speechResponse(w, r, input)
}
