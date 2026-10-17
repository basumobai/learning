package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type examRule struct {
	Seconds  *int              `json:"duration_seconds,omitempty"`
	Total    *float64          `json:"total_points,omitempty"`
	ID       string            `json:"id"`
	Family   string            `json:"family"`
	Revision int               `json:"revision"`
	Subject  string            `json:"subject_id"`
	Region   string            `json:"region"`
	Category string            `json:"category"`
	Paper    string            `json:"paper"`
	From     int               `json:"year_from"`
	To       int               `json:"year_to"`
	Sections []examRuleSection `json:"sections"`
	Sources  []string          `json:"sources"`
	Notes    string            `json:"notes"`
	Verified bool              `json:"verified"`
}
type examRuleSection struct {
	Name   string   `json:"name"`
	Kinds  []string `json:"kinds"`
	Count  *int     `json:"count"`
	Points *float64 `json:"points"`
	Note   string   `json:"note"`
}

func (s *server) curriculumRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/resources/curriculum", s.resourceCurriculum)
	m.HandleFunc("POST /api/v1/admin/resources/terms", s.saveResourceTerm)
	m.HandleFunc("POST /api/v1/admin/resources/chapters", s.saveResourceChapter)
	m.HandleFunc("POST /api/v1/admin/resources/exam-rules", s.saveExamRule)
}
func (s *server) initExamRules(ctx context.Context) error {
	for _, subject := range []string{"english", "math", "physics", "chemistry", "biology", "chinese"} {
		p := platform.GuangdongExam(subject)
		sections := []examRuleSection{}
		for _, section := range p.Sections {
			sections = append(sections, examRuleSection{Name: section.Name, Kinds: section.Kinds, Note: section.Note})
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO learning_exam_rules(id,family,revision,subject_id,region,category,paper,year_from,year_to,sections,sources,notes) VALUES($1,$1,1,$2,'广东','gaokao',$3,2026,2026,$4,$5,$6) ON CONFLICT DO NOTHING`, "gd-2026-"+subject, subject, p.Paper, nullableJSON(sections), nullableJSON(p.Sources), "待核验模板：招生规定支持考试类别；下列板块为平台训练分类，题数和分值未据当年真实试卷核验，不作为官方固定结构。")
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *server) resourceCurriculum(w http.ResponseWriter, r *http.Request) {
	var chapters, rules, terms json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT * FROM learning_chapters ORDER BY subject_id,edition,volume,position,id LIMIT 4000)t`).Scan(&chapters)
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT * FROM learning_exam_rules ORDER BY year_from DESC,family,revision DESC LIMIT 1000)t`).Scan(&rules)
	}
	if err == nil {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT * FROM learning_terms ORDER BY subject_id,kind,name LIMIT 4000)t`).Scan(&terms)
	}
	if err != nil {
		s.dbError(w, "curriculum config", err)
		return
	}
	types := map[string]any{}
	for _, id := range []string{"english", "math", "physics", "chemistry", "biology", "chinese"} {
		types[id] = platform.Types(id)
	}
	respond(w, 200, map[string]any{"chapters": chapters, "rules": rules, "terms": terms, "types": types})
}
func (s *server) saveResourceTerm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Subject  string `json:"subject_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Parent   string `json:"parent_id"`
		Revision int    `json:"revision"`
	}
	if decode(r, &in) != nil || len(platform.Types(in.Subject)) == 0 || in.Kind != "knowledge" && in.Kind != "topic" && in.Kind != "tag" || len([]rune(strings.TrimSpace(in.Name))) < 1 || len([]rune(in.Name)) > 100 || len(in.ID) > 100 {
		fail(w, 400, "知识点字段无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "save term", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(19283745)`); err != nil {
		s.dbError(w, "term tree", err)
		return
	}
	var parent any
	if in.Parent != "" {
		parent = in.Parent
		var valid bool
		tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_terms WHERE id=$1 AND subject_id=$2 AND kind=$3)`, in.Parent, in.Subject, in.Kind).Scan(&valid)
		if !valid {
			fail(w, 400, "上级知识点不存在或学科/类型不匹配")
			return
		}
		if in.ID != "" {
			var cycle bool
			tx.QueryRowContext(r.Context(), `WITH RECURSIVE ancestors AS (SELECT id,parent_id FROM learning_terms WHERE id=$1 UNION SELECT t.id,t.parent_id FROM learning_terms t JOIN ancestors a ON t.id=a.parent_id) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=$2)`, in.Parent, in.ID).Scan(&cycle)
			if cycle {
				fail(w, 400, "知识点层级不能成环")
				return
			}
		}
	}
	if in.ID == "" {
		in.ID = security.Token(18)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_terms(id,subject_id,kind,name,parent_id) VALUES($1,$2,$3,$4,$5)`, in.ID, in.Subject, in.Kind, strings.TrimSpace(in.Name), parent)
	} else {
		res, e := tx.ExecContext(r.Context(), `UPDATE learning_terms SET name=$2,parent_id=$3,revision=revision+1 WHERE id=$1 AND revision=$4 AND subject_id=$5 AND kind=$6`, in.ID, strings.TrimSpace(in.Name), parent, in.Revision, in.Subject, in.Kind)
		err = e
		if err == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				fail(w, 409, "知识点已修改或学科/类型不允许改变")
				return
			}
		}
	}
	if err == nil {
		err = audit(tx, r, "save_knowledge_term", in.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 400, "名称已存在或保存失败，请刷新后检查")
		return
	}
	respond(w, 200, map[string]string{"id": in.ID})
}
func (s *server) saveResourceChapter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Revision int    `json:"revision"`
		Subject  string `json:"subject_id"`
		Edition  string `json:"edition"`
		Volume   string `json:"volume"`
		Title    string `json:"title"`
		Parent   string `json:"parent_id"`
		Position int    `json:"position"`
	}
	if decode(r, &in) != nil || platform.ValidateCurriculum(in.Subject, in.Edition, in.Volume) != nil || strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 200 || len(in.ID) > 100 || in.Position < 0 || in.Position > 10000 {
		fail(w, 400, "请按真实教材填写版本、册次和章节")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "chapter config", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(19283746)`); err != nil {
		s.dbError(w, "chapter tree", err)
		return
	}
	var parent any
	if in.Parent != "" {
		parent = in.Parent
		var valid bool
		tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_chapters WHERE id=$1 AND subject_id=$2 AND edition=$3 AND volume=$4)`, in.Parent, in.Subject, in.Edition, in.Volume).Scan(&valid)
		if !valid {
			fail(w, 400, "上级章节应属于同一教材和册次")
			return
		}
		var cycle bool
		tx.QueryRowContext(r.Context(), `WITH RECURSIVE ancestors AS (SELECT id,parent_id FROM learning_chapters WHERE id=$1 UNION SELECT t.id,t.parent_id FROM learning_chapters t JOIN ancestors a ON t.id=a.parent_id) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=$2)`, in.Parent, in.ID).Scan(&cycle)
		if cycle {
			fail(w, 400, "章节不能成环")
			return
		}
	}
	if in.ID == "" {
		in.ID = security.Token(18)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_chapters(id,subject_id,edition,volume,title,curriculum_chapter,parent_id,position,textbook) VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8)`, in.ID, in.Subject, in.Edition, in.Volume, strings.TrimSpace(in.Title), parent, in.Position, in.Edition+" · "+in.Volume)
	} else {
		res, e := tx.ExecContext(r.Context(), `UPDATE learning_chapters SET title=$2,curriculum_chapter=$2,parent_id=$3,position=$4,revision=revision+1 WHERE id=$1 AND revision=$5 AND subject_id=$6 AND edition=$7 AND volume=$8`, in.ID, strings.TrimSpace(in.Title), parent, in.Position, in.Revision, in.Subject, in.Edition, in.Volume)
		err = e
		if err == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				fail(w, 409, "章节已修改；已有章节不能换到另一教材")
				return
			}
		}
	}
	if err == nil {
		err = audit(tx, r, "save_curriculum_chapter", in.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "chapter config", err)
		return
	}
	respond(w, 200, map[string]string{"id": in.ID})
}
func (in examRule) validate() error {
	if (in.Seconds == nil) != (in.Total == nil) || (in.Seconds != nil && (*in.Seconds < 30 || *in.Seconds > 21600 || *in.Total <= 0 || *in.Total > 1000)) {
		return fmt.Errorf("整卷时限和总分必须一起核验")
	}

	if len(platform.Types(in.Subject)) == 0 || in.Region == "" || len(in.Region) > 50 || in.Category == "" || len(in.Category) > 80 || in.Paper == "" || len(in.Paper) > 200 || in.From < 1977 || in.To < in.From || in.To > 2100 || len(in.Sections) < 1 || len(in.Sections) > 30 || len(in.Sources) < 1 || len(in.Sources) > 10 || len(in.Notes) > 3000 {
		return fmt.Errorf("考试规则字段无效")
	}
	seen := map[string]bool{}
	for _, section := range in.Sections {
		if section.Name == "" || len(section.Name) > 100 || len(section.Kinds) < 1 || len(section.Kinds) > 30 || section.Count != nil && (*section.Count < 0 || *section.Count > 200) || section.Points != nil && (*section.Points < 0 || *section.Points > 750) {
			return fmt.Errorf("考试板块配置无效")
		}
		for _, kind := range section.Kinds {
			if !platform.ValidType(in.Subject, kind) || seen[kind] {
				return fmt.Errorf("题型未知或重复")
			}
			seen[kind] = true
		}
	}
	for _, raw := range in.Sources {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 1000 {
			return fmt.Errorf("请记录可核查的HTTPS来源")
		}
	}
	if in.Verified && len(strings.TrimSpace(in.Notes)) < 15 {
		return fmt.Errorf("确认规则需填写具体核验范围，不能将训练分类当作官方题量")
	}
	return nil
}
func (s *server) saveExamRule(w http.ResponseWriter, r *http.Request) {
	var in examRule
	if decodeLimit(r, &in, 24000) != nil {
		fail(w, 400, "考试规则格式无效")
		return
	}
	if err := in.validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "exam rule", err)
		return
	}
	defer tx.Rollback()
	if in.Family == "" {
		in.Family = security.Token(18)
		in.Revision = 0
	} else {
		if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, "exam:"+in.Family); err != nil {
			s.dbError(w, "exam revision lock", err)
			return
		}
		var latest int
		if err = tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_exam_rules WHERE family=$1 ORDER BY revision DESC LIMIT 1 FOR UPDATE`, in.Family).Scan(&latest); err != nil || latest != in.Revision {
			fail(w, 409, "规则版本已改变，请刷新后创建新修订")
			return
		}
	}
	if in.Revision > 0 {
		var compatible bool
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_exam_rules WHERE family=$1 AND revision=$2 AND subject_id=$3 AND region=$4 AND category=$5 AND year_from=$6 AND year_to=$7)`, in.Family, in.Revision, in.Subject, in.Region, in.Category, in.From, in.To).Scan(&compatible) != nil || !compatible {
			fail(w, 400, "同一规则系列不能变更学科、地区、考试类别或适用年份，请新建系列")
			return
		}
	}

	in.ID = security.Token(18)
	in.Revision++
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_exam_rules(id,family,revision,subject_id,region,category,paper,year_from,year_to,sections,sources,notes,verified,reviewer_id,duration_seconds,total_points) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, in.ID, in.Family, in.Revision, in.Subject, in.Region, in.Category, in.Paper, in.From, in.To, nullableJSON(in.Sections), nullableJSON(in.Sources), in.Notes, in.Verified, userID(r.Context()), in.Seconds, in.Total)
	if err == nil {
		err = audit(tx, r, "create_exam_rule_revision", in.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "exam rule", err)
		return
	}
	respond(w, 201, in)
}
func (s *server) examForYear(ctx context.Context, subject string, year int) map[string]any {
	var raw json.RawMessage
	err := s.db.QueryRowContext(ctx, `SELECT row_to_json(r) FROM learning_exam_rules r WHERE subject_id=$1 AND region='广东' AND year_from<=$2 AND year_to>=$2 AND verified AND NOT EXISTS(SELECT 1 FROM learning_exam_rules newer WHERE newer.family=r.family AND newer.revision>r.revision) ORDER BY created_at DESC,revision DESC LIMIT 1`, subject, year).Scan(&raw)
	if err == nil {
		var out map[string]any
		json.Unmarshal(raw, &out)
		out["policy_year"] = year
		return out
	}
	p := platform.GuangdongExam(subject)
	return map[string]any{"region": "广东", "policy_year": year, "sections": p.Sections, "paper": "平台训练分类（未配置该年已核验规则）", "note": "题型为平台训练标签，题量、分值与顺序须按当年真实试卷核验。", "verified": false, "sources": []string{}}
}
func examYearQuery(r *http.Request) int {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year < 1977 || year > 2100 {
		return time.Now().Year()
	}
	return year
}
