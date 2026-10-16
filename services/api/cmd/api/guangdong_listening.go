package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type speakingTask struct {
	Kind      string   `json:"kind"`
	Cue       string   `json:"cue"`
	Question  string   `json:"question"`
	Reply     string   `json:"reply"`
	Reference string   `json:"reference"`
	Points    []string `json:"points"`
	Evidence  string   `json:"evidence"`
}

const speakingContract = `只返回JSON：{"title":"中文标题","format":"指定的format","scenario":"中文角色与任务说明","transcript":"原创英文听力原文","translation":"中文译文","keywords":["关键词"],"speaking_tasks":[{"kind":"read/ask/answer/retell","cue":"给学生的中文提示","question":"仅answer任务的英文提问","reply":"仅ask任务的计算机英文回答","reference":"英文参考表达","points":["内容要点"],"evidence":"原文或计算机回答中连续逐字依据"}]}。不使用选择题，不声称官方真题。每个任务reference不能为空，points至少一条。`
const guangdongAPrompt = `创建广东高考英语听说Part A模仿朗读专项原创练习。format=guangdong_a。英文原文约100–150词，适合约一分钟自然朗读。speaking_tasks仅1项，kind=read；reference为原文，points指出意群、停顿、重音或连读练习点。`
const guangdongBPrompt = `创建广东高考英语听说Part B角色扮演原创练习。format=guangdong_b；直接听录音，不依赖视频。原文约180–250词，明确角色。严格生成8个任务：先3个ask（cue为中文提问提示，reference为对应英语问句，reply为计算机英文回答），再5个answer（question为英文问题，cue只写“听问题后用英语回答”，reference为英文简答）。5个问题同时考查原始对话和前面3次计算机回答；evidence必须连续逐字出现在原文或3段reply中。包含至少一处时间/数字/地点变更，练习记笔记。`
const guangdongCPrompt = `创建广东高考英语听说Part C故事复述原创练习。format=guangdong_c；原文约200–250词，明确人物、起因、经过、转折和结果，适合播放两遍。keywords给5个英文关键词；speaking_tasks仅1项，kind=retell，cue中文简述任务但不能泄漏故事细节；reference为自然英文复述示例，points给8–10个原文信息点，evidence为原文连续依据。`

func (l listeningLesson) validateSpeaking() error {
	if l.Format != "guangdong_a" && l.Format != "guangdong_b" && l.Format != "guangdong_c" {
		return fmt.Errorf("invalid speaking format")
	}
	if l.Title == "" || len(l.Title) > 300 || len(l.Transcript) < 200 || len(l.Transcript) > 4000 || l.Translation == "" || l.Scenario == "" || len(l.Questions) != 0 || len(l.Keywords) > 10 {
		return fmt.Errorf("incomplete speaking lesson")
	}
	expected := 1
	if l.Format == "guangdong_b" {
		expected = 8
	}
	if len(l.SpeakingTasks) != expected {
		return fmt.Errorf("incorrect Guangdong task count")
	}
	source := l.Transcript
	for _, q := range l.SpeakingTasks {
		source += "\n" + q.Reply
	}
	for i, t := range l.SpeakingTasks {
		kind := "read"
		if l.Format == "guangdong_c" {
			kind = "retell"
		}
		if l.Format == "guangdong_b" {
			kind = "ask"
			if i >= 3 {
				kind = "answer"
			}
		}
		if t.Kind != kind || t.Cue == "" || t.Reference == "" || len(t.Points) < 1 || len(t.Points) > 20 || len(t.Reference) > 4000 || len(t.Question) > 1000 || len(t.Reply) > 2000 {
			return fmt.Errorf("incomplete speaking task")
		}
		if kind == "ask" && t.Reply == "" || kind == "answer" && (t.Question == "" || t.Evidence == "" || !strings.Contains(source, t.Evidence)) {
			return fmt.Errorf("missing role-play reply or evidence")
		}
	}
	return nil
}
func speakingPublic(id string, l listeningLesson, answers []byte) map[string]any {
	result := map[string]any{"id": id, "kind": "listening", "format": l.Format, "title": l.Title, "scenario": l.Scenario, "keywords": l.Keywords, "ai_generated": true}
	tasks := []map[string]any{}
	for i, t := range l.SpeakingTasks {
		cue := t.Cue
		if t.Kind == "answer" {
			cue = "听问题后用英语回答"
		}
		tasks = append(tasks, map[string]any{"index": i, "kind": t.Kind, "cue": cue})
	}
	result["speaking_tasks"] = tasks
	if l.Format == "guangdong_a" {
		result["reading_text"] = l.Transcript
	}
	if len(answers) > 0 {
		var a listeningAnswers
		_ = json.Unmarshal(answers, &a)
		result["responses"] = a.Responses
		result["notes"] = a.Notes
		result["reference_tasks"] = l.SpeakingTasks
		result["transcript"] = l.Transcript
		result["translation"] = l.Translation
		result["submitted"] = true
	}
	return result
}
func speakingSegment(l listeningLesson, segment string) (string, error) {
	if segment == "" || segment == "source" {
		return l.Transcript, nil
	}
	for i, t := range l.SpeakingTasks {
		if segment == fmt.Sprintf("reply-%d", i) && t.Kind == "ask" {
			return t.Reply, nil
		}
		if segment == fmt.Sprintf("question-%d", i) && t.Kind == "answer" {
			return t.Question, nil
		}
	}
	return "", fmt.Errorf("invalid audio segment")
}
func (s *server) speakingFeedback(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ModelID   int64  `json:"model_id"`
		RequestID string `json:"request_id"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.RequestID) {
		fail(w, 400, "评价请求无效")
		return
	}
	var payload, answers []byte
	if s.db.QueryRowContext(r.Context(), `SELECT payload,answers FROM training_sessions WHERE id=$1 AND user_id=$2 AND kind='listening'`, r.PathValue("id"), userID(r.Context())).Scan(&payload, &answers) != nil || len(answers) == 0 {
		fail(w, 409, "请先提交自己的听说作答")
		return
	}
	var l listeningLesson
	var a listeningAnswers
	if json.Unmarshal(payload, &l) != nil || json.Unmarshal(answers, &a) != nil || len(l.SpeakingTasks) == 0 {
		fail(w, 400, "该训练不支持听说评价")
		return
	}
	m, err := s.allowedModel(r.Context(), in.ModelID, "chat")
	if err != nil {
		fail(w, 403, "请选择身份组内授权的评价模型")
		return
	}
	fingerprint := map[string]any{"input": in, "training": r.PathValue("id")}
	if s.aiReplay(w, r, in.RequestID, "speaking_feedback", "", fingerprint) {
		return
	}
	complete := false
	defer func() {
		if !complete {
			s.failAI(r.Context(), in.RequestID)
		}
	}()
	raw, _ := json.Marshal(map[string]any{"lesson": l, "responses": a.Responses})
	text, err := s.completion(r.Context(), m, []chatMessage{{"system", s.taskPrompt(r.Context(), "speaking_feedback") + ` 只返回{"items":[{"index":0,"assessment":"中文具体反馈","suggestion":"英语修改示例"}],"summary":"优先改进建议"}，items必须按任务顺序逐一评价；这是手动转写文本，不含可分析的音频，禁止评价发音、语调或给出官方考试分。`}, {"user", string(raw)}}, true)
	if err != nil {
		providerFailure(w, err)
		return
	}
	var out struct {
		Items []struct {
			Index      int    `json:"index"`
			Assessment string `json:"assessment"`
			Suggestion string `json:"suggestion"`
		} `json:"items"`
		Summary string `json:"summary"`
	}
	if decodeGenerated(text, &out) != nil || len(out.Items) != len(l.SpeakingTasks) || out.Summary == "" {
		fail(w, 502, "评价结构不完整，请更换模型重试")
		return
	}
	for i, item := range out.Items {
		if item.Index != i || item.Assessment == "" || item.Suggestion == "" {
			fail(w, 502, "评价遗漏任务，请重试")
			return
		}
	}
	if _, err = s.allowedModel(r.Context(), in.ModelID, "chat"); err != nil {
		fail(w, 403, "模型授权已更新，请重新选择后评价")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "speaking feedback", err)
		return
	}
	defer tx.Rollback()
	raw, _ = json.Marshal(out)
	_, err = tx.ExecContext(r.Context(), `UPDATE training_sessions SET speaking_feedback=$3 WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context()), string(raw))
	if err == nil {
		err = finishAI(r.Context(), tx, in.RequestID, out)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "speaking feedback", err)
		return
	}
	complete = true
	respond(w, 200, out)
}
