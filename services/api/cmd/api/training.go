package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"example.com/els/api/internal/security"
)

type sentenceChunk struct {
	Text        string `json:"text"`
	Role        string `json:"role"`
	Explanation string `json:"explanation"`
}
type sentenceCheck struct {
	Question    string `json:"question"`
	Answer      string `json:"answer"`
	Explanation string `json:"explanation"`
}
type sentenceAnalysis struct {
	Translation string          `json:"translation"`
	MainClause  string          `json:"main_clause"`
	Chunks      []sentenceChunk `json:"chunks"`
	Grammar     []string        `json:"grammar"`
	Paraphrase  string          `json:"paraphrase"`
	Check       sentenceCheck   `json:"check"`
}

func (a sentenceAnalysis) validate(input string) error {
	if strings.TrimSpace(a.Translation) == "" || a.MainClause == "" || len(a.Chunks) < 1 || len(a.Chunks) > 30 || len(a.Grammar) > 12 || a.Paraphrase == "" || a.Check.Question == "" || a.Check.Answer == "" {
		return fmt.Errorf("incomplete sentence analysis")
	}
	for _, c := range a.Chunks {
		if c.Text == "" || !strings.Contains(input, c.Text) || c.Role == "" || c.Explanation == "" {
			return fmt.Errorf("invalid sentence chunk")
		}
	}
	return nil
}

type listeningQuestion struct {
	Question    string   `json:"question"`
	Choices     []string `json:"choices"`
	Answer      int      `json:"answer"`
	Explanation string   `json:"explanation"`
	Evidence    string   `json:"evidence"`
}
type listeningLesson struct {
	Title       string              `json:"title"`
	Transcript  string              `json:"transcript"`
	Translation string              `json:"translation"`
	Questions   []listeningQuestion `json:"questions"`
}

func (l listeningLesson) validate() error {
	if l.Title == "" || len(l.Title) > 300 || len(l.Transcript) < 200 || len(l.Transcript) > 4000 || l.Translation == "" || len(l.Questions) < 1 || len(l.Questions) > 5 {
		return fmt.Errorf("incomplete listening lesson")
	}
	for _, q := range l.Questions {
		if q.Question == "" || len(q.Choices) != 4 || q.Answer < 0 || q.Answer > 3 || q.Explanation == "" || strings.TrimSpace(q.Evidence) == "" || !strings.Contains(l.Transcript, q.Evidence) {
			return fmt.Errorf("invalid listening question or evidence")
		}
		for _, c := range q.Choices {
			if strings.TrimSpace(c) == "" {
				return fmt.Errorf("empty listening choice")
			}
		}
	}
	return nil
}

const sentencePrompt = `将用户给出的英语句子拆解为高中生能理解的中文讲解。只能返回一个 JSON 对象，不要 Markdown。结构：{"translation":"中文译文","main_clause":"主语 + 谓语 + 宾语/表语的主干说明","chunks":[{"text":"原文中连续的精确片段","role":"主语/谓语/宾语/定语/状语等","explanation":"中文解释作用和连接关系"}],"grammar":["语法要点"],"paraphrase":"更简单的英语表达","check":{"question":"一道中文理解问题","answer":"参考答案","explanation":"依据"}}。每个 chunks.text 必须逐字出现在用户原句中。覆盖关键成分、从句引导词与修饰范围。若输入无法构成正常句子，明确解释缺失成分。`
const listeningPrompt = `创建一份原创的高中英语听力练习，不声称来自教材。只能返回 JSON，不要 Markdown，结构：{"title":"中文标题","transcript":"75–150 个英语单词的自然短文或对话，使用普通标点","translation":"完整中文译文","questions":[{"question":"英文或中文听力问题","choices":["A选项内容","B选项内容","C选项内容","D选项内容"],"answer":0,"explanation":"中文解析，说明为什么对及易错点","evidence":"原文中连续且精确的英语依据"}]}。生成 3 道题，answer 为 0–3 的下标。答案必须仅根据原文能确定；evidence 必须逐字出现在 transcript 中。至少一题训练听取时间/人物/转折或保持关键词，不让四个选项自带答案暗示。`

func (s *server) generateTraining(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ModelID   int64  `json:"model_id"`
		RequestID string `json:"request_id"`
		Kind      string `json:"kind"`
		Text      string `json:"text"`
		Level     string `json:"level"`
	}
	if decodeLimit(r, &input, 32768) != nil || !requestIDPattern.MatchString(input.RequestID) || (input.Kind != "sentence" && input.Kind != "listening") || strings.TrimSpace(input.Text) == "" || len([]rune(input.Text)) > 4000 {
		fail(w, 400, "训练输入无效")
		return
	}
	if input.Kind == "listening" && (len([]rune(input.Text)) > 200 || (input.Level != "A2" && input.Level != "B1" && input.Level != "B2")) {
		fail(w, 400, "听力主题最多 200 字符，难度请选择 A2/B1/B2")
		return
	}
	m, err := s.allowedModel(r.Context(), input.ModelID, "chat")
	if err != nil {
		fail(w, 403, "当前权限组未授权这个聊天模型，或服务尚未配置")
		return
	}
	if s.aiReplay(w, r, input.RequestID, input.Kind, "", input) {
		return
	}
	complete := false
	defer func() {
		if !complete {
			s.failAI(r.Context(), input.RequestID)
		}
	}()
	prompt, content := s.taskPrompt(r.Context(), "sentence"), input.Text
	if input.Kind == "listening" {
		prompt = s.taskPrompt(r.Context(), "listening")
		content = "难度：" + input.Level + "。主题：" + input.Text
	}
	text, err := s.completion(r.Context(), m, []chatMessage{{"system", tutorPrompt + "\n" + prompt}, {"user", content}}, true)
	if err != nil {
		providerFailure(w, err)
		return
	}
	var payload any
	title := ""
	if input.Kind == "sentence" {
		var a sentenceAnalysis
		err = decodeGenerated(text, &a)
		if err == nil {
			err = a.validate(input.Text)
		}
		payload = map[string]any{"text": input.Text, "analysis": a}
		title = string([]rune(input.Text)[:min(len([]rune(input.Text)), 70)])
	} else {
		var lesson listeningLesson
		err = decodeGenerated(text, &lesson)
		if err == nil {
			err = lesson.validate()
		}
		payload = lesson
		title = lesson.Title
	}
	if err != nil {
		fail(w, 502, "模型返回的训练结构或答案依据不完整，请重试或更换模型")
		return
	}
	id := security.Token(18)
	raw, _ := json.Marshal(payload)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "training save", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO training_sessions(id,user_id,kind,title,payload) VALUES($1,$2,$3,$4,$5)`, id, userID(r.Context()), input.Kind, title, string(raw))
	result := trainingPublic(id, input.Kind, raw, nil)
	if err == nil {
		err = finishAI(r.Context(), tx, input.RequestID, result)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "training commit", err)
		return
	}
	complete = true
	respond(w, 200, result)
}

type listeningAnswers struct {
	RequestID string `json:"request_id"`
	Answers   []int  `json:"answers"`
}

func trainingPublic(id, kind string, payload, answers []byte) map[string]any {
	result := map[string]any{"id": id, "kind": kind, "ai_generated": true}
	if kind == "sentence" {
		result["data"] = json.RawMessage(payload)
		return result
	}
	var lesson listeningLesson
	_ = json.Unmarshal(payload, &lesson)
	result["title"] = lesson.Title
	questions := []map[string]any{}
	for i, q := range lesson.Questions {
		questions = append(questions, map[string]any{"index": i, "question": q.Question, "choices": q.Choices})
	}
	result["questions"] = questions
	if len(answers) > 0 {
		var a listeningAnswers
		_ = json.Unmarshal(answers, &a)
		score := 0
		feedback := []map[string]any{}
		for i, q := range lesson.Questions {
			picked := -1
			if i < len(a.Answers) {
				picked = a.Answers[i]
			}
			correct := picked == q.Answer
			if correct {
				score++
			}
			feedback = append(feedback, map[string]any{"picked": picked, "answer": q.Answer, "correct": correct, "explanation": q.Explanation, "evidence": q.Evidence})
		}
		result["feedback"] = feedback
		result["score"] = score
		result["transcript"] = lesson.Transcript
		result["translation"] = lesson.Translation
	}
	return result
}
func (s *server) training(w http.ResponseWriter, r *http.Request) {
	var kind string
	var payload, answers []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT kind,payload,answers FROM training_sessions WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&kind, &payload, &answers)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "训练不存在")
		return
	}
	if err != nil {
		s.dbError(w, "training restore", err)
		return
	}
	respond(w, 200, trainingPublic(r.PathValue("id"), kind, payload, answers))
}
func (s *server) trainingHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,kind,title,created_at FROM training_sessions WHERE user_id=$1 ORDER BY created_at DESC`, userID(r.Context()))
	if err != nil {
		s.dbError(w, "training history", err)
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, kind, title string
		var date any
		if err = rows.Scan(&id, &kind, &title, &date); err != nil {
			s.dbError(w, "training history row", err)
			return
		}
		result = append(result, map[string]any{"id": id, "kind": kind, "title": title, "created_at": date})
	}
	if rows.Err() != nil {
		s.dbError(w, "training history rows", rows.Err())
		return
	}
	respond(w, 200, map[string]any{"training": result})
}
func (s *server) listeningAnswer(w http.ResponseWriter, r *http.Request) {
	var input listeningAnswers
	if decode(r, &input) != nil || !requestIDPattern.MatchString(input.RequestID) {
		fail(w, 400, "答案格式无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "listening answer", err)
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var payload, old []byte
	err = tx.QueryRowContext(r.Context(), `SELECT payload,answers FROM training_sessions WHERE id=$1 AND user_id=$2 AND kind='listening' FOR UPDATE`, id, userID(r.Context())).Scan(&payload, &old)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "听力练习不存在")
		return
	}
	if err != nil {
		s.dbError(w, "listening lesson", err)
		return
	}
	var lesson listeningLesson
	if json.Unmarshal(payload, &lesson) != nil || len(input.Answers) != len(lesson.Questions) {
		fail(w, 400, "请完成所有题目")
		return
	}
	for _, n := range input.Answers {
		if n < 0 || n > 3 {
			fail(w, 400, "选项无效")
			return
		}
	}
	raw, _ := json.Marshal(input)
	if len(old) > 0 {
		var previous listeningAnswers
		_ = json.Unmarshal(old, &previous)
		before, _ := json.Marshal(previous)
		if string(before) != string(raw) {
			fail(w, 409, "这份练习已提交，请查看原文和解析")
			return
		}
		respond(w, 200, trainingPublic(id, "listening", payload, old))
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE training_sessions SET answers=$3 WHERE id=$1 AND user_id=$2`, id, userID(r.Context()), string(raw))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "listening commit", err)
		return
	}
	respond(w, 200, trainingPublic(id, "listening", payload, raw))
}
