package main

import (
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"net/http"
	"sort"
)

type selectionCandidate struct {
	ID       string   `json:"question_id"`
	Revision int      `json:"question_revision"`
	Subject  string   `json:"subject_id"`
	Kind     string   `json:"kind"`
	Attempts int      `json:"prior_attempts"`
	State    string   `json:"skill_state"`
	Weight   int      `json:"weight"`
	Reasons  []string `json:"reasons"`
}

func (s *server) planningRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/learning/recommendations", s.recommendPractice)
	m.HandleFunc("GET /api/v1/learning/recommendations/{id}", s.replaySelection)
	m.HandleFunc("GET /api/v1/learning/report", s.learningReport)
	m.HandleFunc("GET /api/v1/learning/plans/today", s.todayPlan)
	m.HandleFunc("POST /api/v1/learning/plans", s.createStudyPlan)
	m.HandleFunc("PATCH /api/v1/learning/plans/{id}", s.editStudyPlan)
	s.reviewItemRoutes(m)
}
func (s *server) replaySelection(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(d)-'digest'-'user_id' FROM learning_selection_decisions d WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context())).Scan(&raw)
	if err != nil {
		fail(w, 404, "选题记录不存在")
		return
	}
	respond(w, 200, raw)
}
func (s *server) recommendPractice(w http.ResponseWriter, r *http.Request) {
	if !v3Enabled("PLANNING") {
		fail(w, 503, "智能强化尚未启用，自主练习仍可使用")
		return
	}
	var in struct {
		Request  string   `json:"request_id"`
		Subjects []string `json:"subjects"`
		Excluded []string `json:"exclude_question_ids"`
		Limit    int      `json:"limit"`
	}
	if decodeLimit(r, &in, 12000) != nil || !requestIDPattern.MatchString(in.Request) || len(in.Subjects) > 6 || len(in.Excluded) > 200 {
		fail(w, 400, "选题范围无效")
		return
	}
	if in.Limit == 0 {
		in.Limit = 5
	}
	if in.Limit < 1 || in.Limit > 20 {
		fail(w, 400, "题量无效")
		return
	}
	in.Subjects = canonicalIDs(in.Subjects)
	in.Excluded = canonicalIDs(in.Excluded)
	for _, subject := range in.Subjects {
		if len(platform.Types(subject)) == 0 {
			fail(w, 400, "科目无效")
			return
		}
	}
	digest := payloadDigest(in)
	var id, old string
	err := s.db.QueryRowContext(r.Context(), `SELECT id,digest FROM learning_selection_decisions WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &old)
	if err == nil {
		if old != digest {
			fail(w, 409, "同键选题条件发生改变")
			return
		}
		r.SetPathValue("id", id)
		s.replaySelection(w, r)
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "recommend replay", err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT q.id,q.revision,q.subject_id,q.kind,
 (SELECT count(*) FROM learning_attempts a WHERE a.user_id=$1 AND a.question_id=q.id),
 COALESCE((SELECT st.state FROM learning_user_skill_states st JOIN learning_skill_link_revisions l ON l.id=(SELECT id FROM learning_skill_link_revisions WHERE question_id=q.id AND question_revision=q.revision ORDER BY revision DESC LIMIT 1) CROSS JOIN jsonb_array_elements(l.links) x WHERE st.user_id=$1 AND st.skill_id=x->>'skill_id' AND x->>'role'='primary' ORDER BY CASE st.state WHEN 'learning' THEN 0 WHEN 'consolidating' THEN 1 WHEN 'limited' THEN 2 ELSE 3 END LIMIT 1),'limited'),
 EXISTS(SELECT 1 FROM learning_scoring_rule_revisions WHERE question_id=q.id AND question_revision=q.revision),
 EXISTS(SELECT 1 FROM learning_skill_link_revisions WHERE question_id=q.id AND question_revision=q.revision)
 FROM learning_questions q WHERE q.status='published' AND (cardinality($2::text[])=0 OR q.subject_id=ANY($2)) ORDER BY q.id LIMIT 1000`, userID(r.Context()), in.Subjects)
	if err != nil {
		s.dbError(w, "recommend candidates", err)
		return
	}
	all := []selectionCandidate{}
	excluded := []map[string]string{}
	skip := map[string]bool{}
	for _, id := range in.Excluded {
		skip[id] = true
	}
	for rows.Next() {
		var c selectionCandidate
		var rule, links bool
		if err = rows.Scan(&c.ID, &c.Revision, &c.Subject, &c.Kind, &c.Attempts, &c.State, &rule, &links); err != nil {
			break
		}
		why := ""
		if skip[c.ID] {
			why = "学生主动排除"
		} else if !rule || !links {
			why = "缺少已审核评分或知识映射"
		}
		if why != "" {
			excluded = append(excluded, map[string]string{"question_id": c.ID, "reason": why})
			continue
		}
		c.Reasons = []string{}
		c.Weight = 10 - c.Attempts*20
		if c.Attempts == 0 {
			c.Weight += 40
			c.Reasons = append(c.Reasons, "未做过，适合检验迁移")
		}
		switch c.State {
		case "learning":
			c.Weight += 60
			c.Reasons = append(c.Reasons, "该知识点的独立证据显示仍需练习")
		case "consolidating":
			c.Weight += 30
			c.Reasons = append(c.Reasons, "巩固已有独立证据")
		case "limited":
			c.Weight += 15
			c.Reasons = append(c.Reasons, "证据有限，补充一次独立尝试")
		}
		all = append(all, c)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		s.dbError(w, "recommend scan", err)
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Weight != all[j].Weight {
			return all[i].Weight > all[j].Weight
		}
		return security.Digest(all[i].ID+in.Request) < security.Digest(all[j].ID+in.Request)
	})
	selected := all
	if len(selected) > in.Limit {
		selected = selected[:in.Limit]
	}
	id = security.Token(18)
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO learning_selection_decisions(id,user_id,request_id,digest,policy_version,input,candidates,excluded,selected) VALUES($1,$2,$3,$4,'selection-v1',$5,$6,$7,$8) ON CONFLICT(user_id,request_id) DO NOTHING`, id, userID(r.Context()), in.Request, digest, nullableJSON(in), nullableJSON(all), nullableJSON(excluded), nullableJSON(selected))
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT id,digest FROM learning_selection_decisions WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&id, &old)
	}
	if err != nil {
		s.dbError(w, "save recommendation", err)
		return
	}
	if old != digest {
		fail(w, 409, "选题请求冲突")
		return
	}
	r.SetPathValue("id", id)
	s.replaySelection(w, r)
}
func (s *server) learningReport(w http.ResponseWriter, r *http.Request) {
	var summary, subjects, skills json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT jsonb_build_object('submitted',count(*),'independent',count(*) FILTER(WHERE transfer_from_id IS NULL AND NOT EXISTS(SELECT 1 FROM learning_attempts p WHERE p.user_id=a.user_id AND p.question_id=a.question_id AND (p.submitted_at,p.id)<(a.submitted_at,a.id)) AND NOT EXISTS(SELECT 1 FROM learning_hint_events h WHERE h.attempt_id=a.id)), 'hinted',count(*) FILTER(WHERE EXISTS(SELECT 1 FROM learning_hint_events h WHERE h.attempt_id=a.id)), 'transfer',count(*) FILTER(WHERE transfer_from_id IS NOT NULL),'seconds',COALESCE(sum(LEAST(7200,GREATEST(0,extract(epoch FROM submitted_at-started_at)))),0)) FROM learning_attempts a WHERE user_id=$1 AND submitted_at>=now()-interval '7 days'`, userID(r.Context())).Scan(&summary)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM (SELECT q.subject_id,count(*) AS submitted,count(*) FILTER(WHERE e.confidence IN ('verified_rule','human_confirmed')) AS verified_scores,count(*) FILTER(WHERE e.confidence IN ('unresolved','legacy') OR e.id IS NULL) AS limited_scores FROM learning_attempts a JOIN learning_question_revisions q ON q.question_id=a.question_id AND q.revision=a.question_revision LEFT JOIN LATERAL(SELECT e.* FROM learning_score_evidence e WHERE e.attempt_id=a.id AND NOT EXISTS(SELECT 1 FROM learning_score_evidence n WHERE n.supersedes_id=e.id) ORDER BY created_at DESC,id DESC LIMIT 1)e ON true WHERE a.user_id=$1 AND a.submitted_at>=now()-interval '7 days' GROUP BY q.subject_id)t`, userID(r.Context())).Scan(&subjects)
	}
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(t)-'user_id'),'[]') FROM (SELECT s.*,k.name,k.subject_id FROM learning_user_skill_states s JOIN learning_terms k ON k.id=s.skill_id WHERE s.user_id=$1 ORDER BY k.subject_id,k.name)t`, userID(r.Context())).Scan(&skills)
	}
	if err != nil {
		s.dbError(w, "learning report", err)
		return
	}
	respond(w, 200, map[string]any{"period": "rolling_7_days", "summary": summary, "subjects": subjects, "skills": skills, "note": "独立/复答/提示与迁移分开统计；未评分与旧版反馈不视为高可信能力。耗时为开始至交卷墙钟时间，单次最多计2小时。"})
}

type planItem struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Subject string `json:"subject_id,omitempty"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
	Skipped bool   `json:"skipped"`
}

func (s *server) todayPlan(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(p)-'user_id' FROM learning_study_plans p WHERE user_id=$1 AND day=(now() AT TIME ZONE 'Asia/Shanghai')::date`, userID(r.Context())).Scan(&raw)
	if err == sql.ErrNoRows {
		respond(w, 200, map[string]any{"plan": nil})
		return
	}
	if err != nil {
		s.dbError(w, "today plan", err)
		return
	}
	respond(w, 200, map[string]any{"plan": raw})
}
func (s *server) createStudyPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Minutes  int      `json:"minutes"`
		Subjects []string `json:"subjects"`
	}
	if decode(r, &in) != nil || in.Minutes < 5 || in.Minutes > 240 || len(in.Subjects) > 6 {
		fail(w, 400, "请选择5至240分钟及学习科目")
		return
	}
	in.Subjects = canonicalIDs(in.Subjects)
	for _, s := range in.Subjects {
		if len(platform.Types(s)) == 0 {
			fail(w, 400, "学科无效")
			return
		}
	}
	items := []planItem{}
	remaining := in.Minutes
	var due int
	err := s.db.QueryRowContext(r.Context(), `SELECT (SELECT count(*) FROM learning_review_items WHERE user_id=$1 AND due<=now())+(SELECT count(*) FROM review_states WHERE user_id=$1 AND next_review_at<=now())`, userID(r.Context())).Scan(&due)
	if err != nil {
		s.dbError(w, "plan due", err)
		return
	}
	if due > 0 {
		n := min(5, remaining)
		items = append(items, planItem{ID: "recall", Kind: "review", Minutes: n, Reason: "有到期主动回忆项目；词汇和自建卡分开复习"})
		remaining -= n
	}
	for i, subject := range in.Subjects {
		n := remaining / (len(in.Subjects) - i)
		if n == 0 {
			break
		}
		items = append(items, planItem{ID: "practice-" + subject, Kind: "practice", Subject: subject, Minutes: n, Reason: "你选择的学科；可自由练习或从证据建议开始"})
		remaining -= n
	}
	if len(items) == 0 {
		items = append(items, planItem{ID: "free", Kind: "free", Minutes: remaining, Reason: "自主选择今天要练习的内容"})
	}
	var id string
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO learning_study_plans(id,user_id,day,minutes,items) VALUES($1,$2,(now() AT TIME ZONE 'Asia/Shanghai')::date,$3,$4) ON CONFLICT(user_id,day) DO UPDATE SET day=EXCLUDED.day RETURNING id`, security.Token(18), userID(r.Context()), in.Minutes, nullableJSON(items)).Scan(&id)
	if err != nil {
		s.dbError(w, "create plan", err)
		return
	}
	s.todayPlan(w, r)
}
func (s *server) editStudyPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Request string   `json:"request_id"`
		Version int      `json:"version"`
		Skip    []string `json:"skip_ids"`
	}
	if decode(r, &in) != nil || !requestIDPattern.MatchString(in.Request) || len(in.Skip) > 20 {
		fail(w, 400, "计划修改无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "edit plan", err)
		return
	}
	defer tx.Rollback()
	var raw []byte
	var version int
	err = tx.QueryRowContext(r.Context(), `SELECT items,version FROM learning_study_plans WHERE id=$1 AND user_id=$2 FOR UPDATE`, r.PathValue("id"), userID(r.Context())).Scan(&raw, &version)
	if err != nil {
		fail(w, 404, "计划不存在")
		return
	}
	var old string
	err = tx.QueryRowContext(r.Context(), `SELECT digest FROM learning_plan_events WHERE user_id=$1 AND request_id=$2`, userID(r.Context()), in.Request).Scan(&old)
	if err == nil {
		if old != payloadDigest(in) {
			fail(w, 409, "计划请求冲突")
			return
		}
		respond(w, 200, map[string]bool{"saved": true})
		return
	}
	if err != sql.ErrNoRows {
		s.dbError(w, "plan replay", err)
		return
	}
	if version != in.Version {
		fail(w, 409, "计划已改变，请刷新")
		return
	}
	var items []planItem
	json.Unmarshal(raw, &items)
	skip := map[string]bool{}
	for _, id := range in.Skip {
		skip[id] = true
	}
	for i := range items {
		items[i].Skipped = skip[items[i].ID]
		delete(skip, items[i].ID)
	}
	if len(skip) > 0 {
		fail(w, 400, "未知计划项目")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE learning_study_plans SET items=$3,version=version+1 WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userID(r.Context()), nullableJSON(items))
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_plan_events(id,plan_id,user_id,request_id,digest,body) VALUES($1,$2,$3,$4,$5,$6)`, security.Token(18), r.PathValue("id"), userID(r.Context()), in.Request, payloadDigest(in), nullableJSON(in))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save plan", err)
		return
	}
	respond(w, 200, map[string]any{"saved": true, "version": version + 1})
}
