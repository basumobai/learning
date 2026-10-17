package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type reviewSelection struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}
type publishRequest struct {
	Document string            `json:"document_id"`
	Revision int               `json:"revision"`
	Run      string            `json:"run_id"`
	Units    []reviewSelection `json:"units"`
	Note     string            `json:"note"`
	Replace  bool              `json:"replace"`
}

func (s *server) reviewRoutes(m *http.ServeMux) {
	m.HandleFunc("PATCH /api/v1/admin/resources/{id}/units/{unit}", s.editResourceUnit)
	m.HandleFunc("POST /api/v1/admin/resources/{id}/approve", s.approveResource)
	m.HandleFunc("POST /api/v1/admin/resources/{id}/return", s.returnResource)
	m.HandleFunc("GET /api/v1/resources/library", s.publishedResources)
	m.HandleFunc("GET /api/v1/resources/library/{id}", s.publishedResourceDetail)
	m.HandleFunc("GET /api/v1/resources/library/assets/{asset}", s.publicationAsset)
}
func loadEvidence(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, run, kind string) (map[int]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT result FROM learning_analysis_steps WHERE run_id=$1 AND step_key LIKE $2`, run, "page:"+kind+":%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pages := map[int]string{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p samplePage
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		pages[p.Page] = p.Text
	}
	return pages, rows.Err()
}
func validateUnitLinks(ctx context.Context, tx *sql.Tx, u deepUnit) error {
	seen := map[string]bool{}
	for _, id := range u.Knowledge {
		if seen[id] {
			return fmt.Errorf("知识点重复")
		}
		seen[id] = true
		var valid bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_terms WHERE id=$1 AND kind='knowledge' AND (subject_id=$2 OR subject_id IS NULL))`, id, u.Subject).Scan(&valid) != nil || !valid {
			return fmt.Errorf("知识点不存在或学科不匹配")
		}
	}
	seen = map[string]bool{}
	for _, id := range u.Chapters {
		if seen[id] {
			return fmt.Errorf("章节重复")
		}
		seen[id] = true
		var valid bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_chapters WHERE id=$1 AND subject_id=$2)`, id, u.Subject).Scan(&valid) != nil || !valid {
			return fmt.Errorf("章节不存在或学科不匹配")
		}
	}
	return nil
}
func (s *server) editResourceUnit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision         int      `json:"revision"`
		ResourceRevision int      `json:"resource_revision"`
		Body             deepUnit `json:"body"`
	}
	if decodeLimit(r, &in, 180000) != nil || in.Revision < 1 || in.ResourceRevision < 1 {
		fail(w, 400, "内容版本或格式无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "edit unit", err)
		return
	}
	defer tx.Rollback()
	var rev int
	if tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&rev) != nil {
		fail(w, 404, "资源不存在")
		return
	}
	if rev != in.ResourceRevision {
		fail(w, 409, "资源已改变，请刷新")
		return
	}
	var run string
	var unitRevision int
	if tx.QueryRowContext(r.Context(), `SELECT run_id,revision FROM learning_content_units WHERE id=$1 AND document_id=$2 FOR UPDATE`, r.PathValue("unit"), r.PathValue("id")).Scan(&run, &unitRevision) != nil {
		fail(w, 404, "内容单元不存在")
		return
	}
	if unitRevision != in.Revision {
		fail(w, 409, "内容已改变，请刷新")
		return
	}
	pages, err := loadEvidence(r.Context(), tx, run, "question")
	if err == nil {
		err = validateUnitDraft(in.Body, pages)
	}
	if err == nil {
		err = validateUnitLinks(r.Context(), tx, in.Body)
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if in.Body.AnswerOrigin != "" && in.Body.AnswerOrigin != "reference" && in.Body.AnswerOrigin != "human" || len(in.Body.HumanAnswerNote) > 2000 || len(in.Body.VerificationSource) > 1000 {
		fail(w, 400, "答案或真题核验说明无效")
		return
	}
	if in.Body.Authenticity != "unverified" && in.Body.Authenticity != "verified" && in.Body.Authenticity != "not_official" {
		fail(w, 400, "真实性状态无效")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE learning_content_units SET body=$2,revision=revision+1,review_status='pending' WHERE id=$1`, r.PathValue("unit"), nullableJSON(in.Body))
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_resource_reviews(document_id,run_id,unit_id,reviewer_id,action,revision,note) VALUES($1,$2,$3,$4,'edit',$5,'对照原件修正')`, r.PathValue("id"), run, r.PathValue("unit"), userID(r.Context()), in.Revision+1)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "edit unit", err)
		return
	}
	respond(w, 200, map[string]int{"revision": in.Revision + 1})
}
func (s *server) approveResource(w http.ResponseWriter, r *http.Request) {
	var in publishRequest
	if decodeLimit(r, &in, 24000) != nil || in.Revision < 1 || len(in.Units) < 1 || len(in.Units) > 100 || len(in.Note) > 2000 || strings.TrimSpace(in.Note) == "" {
		fail(w, 400, "请选择1至100个内容单元并填写审核说明")
		return
	}
	in.Document = r.PathValue("id")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "review queue", err)
		return
	}
	defer tx.Rollback()
	var rev int
	if tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, in.Document).Scan(&rev) != nil {
		fail(w, 404, "资源不存在")
		return
	}
	if rev != in.Revision {
		fail(w, 409, "资源已修改，请刷新")
		return
	}
	var ready bool
	if tx.QueryRowContext(r.Context(), `SELECT status='ready' AND resource_revision=$3 AND (job_id IS NULL OR EXISTS(SELECT 1 FROM learning_jobs j WHERE j.id=job_id AND j.status='completed')) FROM learning_analysis_runs WHERE id=$1 AND document_id=$2`, in.Run, in.Document, rev).Scan(&ready) != nil || !ready {
		fail(w, 409, "完整解析尚未完成或资源版本已改变")
		return
	}
	seen := map[string]bool{}
	for _, u := range in.Units {
		if u.Revision < 1 || seen[u.ID] {
			fail(w, 400, "内容选择重复或版本无效")
			return
		}
		seen[u.ID] = true
		var current int
		if tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_content_units WHERE id=$1 AND run_id=$2`, u.ID, in.Run).Scan(&current) != nil || current != u.Revision {
			fail(w, 409, "内容已修改，请重新选择")
			return
		}
	}
	key := security.Digest(stringMustMarshal(in))
	var id string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM learning_jobs WHERE resource_id=$1 AND kind='resource_publish' AND dedupe_key=$2`, in.Document, key).Scan(&id)
	if err == sql.ErrNoRows {
		id = security.Token(18)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_jobs(id,owner_id,kind,dedupe_key,payload,resource_id,resource_revision) VALUES($1,$2,'resource_publish',$3,$4,$5,$6)`, id, userID(r.Context()), key, nullableJSON(in), in.Document, rev)
	}
	if err == nil {
		err = audit(tx, r, "queue_resource_publication", in.Document)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "review queue", err)
		return
	}
	respond(w, 202, map[string]string{"job_id": id, "message": "已排队校验和发布；校验失败不会改变正式内容"})
}
func validatePublishUnit(ctx context.Context, tx *sql.Tx, u deepUnit, run string) error {
	pages, err := loadEvidence(ctx, tx, run, "question")
	if err != nil {
		return err
	}
	if err = validateUnitDraft(u, pages); err != nil {
		return err
	}
	if err = validateUnitLinks(ctx, tx, u); err != nil {
		return err
	}
	if u.Type == "official_paper" {
		if u.Authenticity != "verified" || u.ExamYear == nil || u.ExamRegion == "" || u.ExamName == "" || len(strings.TrimSpace(u.VerificationSource)) < 12 {
			return fmt.Errorf("真题需人工核验名称、年份、地区及来源，文件名不足以确认为真题")
		}
		if !strings.HasPrefix(u.VerificationSource, "https://") {
			return fmt.Errorf("请记录核验真题的HTTPS来源")
		}
		var valid bool
		if tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_exam_rules r WHERE id=$1 AND subject_id=$2 AND region=$3 AND year_from<=$4 AND year_to>=$4 AND verified AND NOT EXISTS(SELECT 1 FROM learning_exam_rules newer WHERE newer.family=r.family AND newer.revision>r.revision))`, u.RuleID, u.Subject, u.ExamRegion, *u.ExamYear).Scan(&valid) != nil || !valid {
			return fmt.Errorf("请关联已核验且年份、地区、学科匹配的考试规则")
		}
	}
	if u.Kind != "question" {
		return nil
	}
	if err = platform.ValidateQuestion(u.Subject, u.QuestionType, u.Content, u.Solution); err != nil {
		return err
	}
	if len(u.Knowledge) == 0 {
		return fmt.Errorf("题目至少需核验一个知识点")
	}
	if u.AnswerOrigin == "human" {
		if len(strings.TrimSpace(u.HumanAnswerNote)) < 10 {
			return fmt.Errorf("人工解答需记录核验依据")
		}
	} else {
		if len(u.AnswerEvidence) == 0 {
			return fmt.Errorf("缺少原始答案依据，请人工核验或保留待审")
		}
		for _, kind := range []string{"question", "answer"} {
			refs := []sourceEvidence{}
			for _, e := range u.AnswerEvidence {
				if e.Kind == kind {
					refs = append(refs, e)
				} else if e.Kind != "question" && e.Kind != "answer" {
					return fmt.Errorf("答案来源类型无效")
				}
			}
			if len(refs) == 0 {
				continue
			}
			texts, e := loadEvidence(ctx, tx, run, kind)
			if e != nil {
				return e
			}
			if e = validateReference(deepReference{Key: u.Key, Solution: u.Solution, Evidence: refs, Confidence: u.Confidence}, texts, kind); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *server) publishResource(ctx context.Context, payload []byte) (any, error) {
	var in publishRequest
	if json.Unmarshal(payload, &in) != nil {
		return nil, fmt.Errorf("发布参数无效")
	}
	type selected struct {
		ID       string
		Revision int
		Key      string
		Body     deepUnit
		Images   [][]byte
	}
	units := []selected{}
	assetBytes, assetCount := 0, 0
	// Render outside the transaction. Final resource/unit revisions are checked again.
	for _, u := range in.Units {
		var raw []byte
		var actual int
		var key string
		if err := s.db.QueryRowContext(ctx, `SELECT body,revision,unit_key FROM learning_content_units WHERE id=$1 AND run_id=$2 AND document_id=$3`, u.ID, in.Run, in.Document).Scan(&raw, &actual, &key); err != nil || actual != u.Revision {
			return nil, fmt.Errorf("内容已变化，请重新审核")
		}
		var body deepUnit
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		v := selected{ID: u.ID, Revision: actual, Key: key, Body: body}
		for _, f := range body.Figures {
			img, err := s.pageImage(ctx, in.Document, "question", f.Page)
			if err != nil {
				return nil, err
			}
			cropped, err := cropPNG(img, f.BBox)
			if err != nil {
				return nil, err
			}
			assetBytes += len(cropped)
			assetCount++
			if assetBytes > 64*1024*1024 || assetCount > 200 {
				return nil, fmt.Errorf("图片超过本批审核内存上限，请减少所选内容")
			}
			v.Images = append(v.Images, cropped)
		}
		units = append(units, v)
		if err := s.jobProgress(ctx, map[string]any{"validated_assets": len(units), "total_units": len(in.Units)}); err != nil {
			return nil, err
		}
	}
	tx, err := s.learningWriteTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, in.Document).Scan(&version); err != nil || version != in.Revision {
		return nil, errStaleJob
	}
	var ready bool
	if tx.QueryRowContext(ctx, `SELECT status='ready' AND resource_revision=$3 FROM learning_analysis_runs WHERE id=$1 AND document_id=$2`, in.Run, in.Document, version).Scan(&ready) != nil || !ready {
		return nil, fmt.Errorf("解析版本未完成或已过期")
	}
	for _, u := range units {
		var rev int
		if tx.QueryRowContext(ctx, `SELECT revision FROM learning_content_units WHERE id=$1 FOR UPDATE`, u.ID).Scan(&rev) != nil || rev != u.Revision {
			return nil, fmt.Errorf("审核内容已变化")
		}
		if err = validatePublishUnit(ctx, tx, u.Body, in.Run); err != nil {
			return nil, fmt.Errorf("%s：%w", u.Body.Title, err)
		}
	}
	if in.Replace {
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM learning_content_units WHERE run_id=$1 AND review_status='pending' AND NOT(id=ANY($2::text[]))`, in.Run, selectionIDs(in.Units)).Scan(&pending); err != nil {
			return nil, err
		}
		if pending > 0 {
			return nil, fmt.Errorf("替换正式版本前请完成其余内容的审核或退回")
		}
	}
	published := 0
	for _, u := range units {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_unit_publications WHERE unit_id=$1 AND revision=$2 AND active)`, u.ID, u.Revision).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		var qid any
		if u.Body.Kind == "question" {
			var chapter any
			if len(u.Body.Chapters) > 0 {
				chapter = u.Body.Chapters[0]
			}
			id := security.Token(18)
			provenance := map[string]any{"resource_id": in.Document, "run_id": in.Run, "unit_id": u.ID, "question_pages": u.Body.Pages, "evidence": u.Body.Evidence, "answer_evidence": u.Body.AnswerEvidence, "confidence": u.Body.Confidence, "knowledge_ids": u.Body.Knowledge, "resource_type": u.Body.Type, "difficulty": u.Body.Difficulty, "exam_rule_id": u.Body.RuleID, "answer_origin": u.Body.AnswerOrigin, "human_answer_note": u.Body.HumanAnswerNote, "reviewer_id": userID(ctx), "needs_review": false}
			var revision int
			err = tx.QueryRowContext(ctx, `INSERT INTO learning_questions(id,subject_id,chapter_id,kind,content,solution,provenance,document_id,source_key,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'published') ON CONFLICT(document_id,source_key) DO UPDATE SET subject_id=EXCLUDED.subject_id,chapter_id=EXCLUDED.chapter_id,kind=EXCLUDED.kind,content=EXCLUDED.content,solution=EXCLUDED.solution,provenance=EXCLUDED.provenance,status='published',revision=learning_questions.revision+1 RETURNING id,revision`, id, u.Body.Subject, chapter, u.Body.QuestionType, nullableJSON(u.Body.Content), nullableJSON(u.Body.Solution), nullableJSON(provenance), in.Document, "v2:"+u.Key).Scan(&id, &revision)
			if err != nil {
				return nil, err
			}
			qid = id
			// New revisions get exactly the reviewed figure set; old attempt assets survive.
			if _, err = tx.ExecContext(ctx, `DELETE FROM learning_revision_assets WHERE question_id=$1 AND revision=$2`, id, revision); err != nil {
				return nil, err
			}
			for i, f := range u.Body.Figures {
				_, err = tx.ExecContext(ctx, `INSERT INTO learning_assets(id,question_id,image,alt,source_page,bbox) VALUES($1,$2,$3,$4,$5,$6)`, security.Token(18), id, u.Images[i], f.Alt, f.Page, nullableJSON(f.BBox))
				if err != nil {
					return nil, err
				}
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM learning_question_terms WHERE question_id=$1`, id); err != nil {
				return nil, err
			}
			for _, term := range u.Body.Knowledge {
				if _, err = tx.ExecContext(ctx, `INSERT INTO learning_question_terms(question_id,term_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, term); err != nil {
					return nil, err
				}
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM learning_question_chapters WHERE question_id=$1`, id); err != nil {
				return nil, err
			}
			for _, ch := range u.Body.Chapters {
				if _, err = tx.ExecContext(ctx, `INSERT INTO learning_question_chapters(question_id,chapter_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, ch); err != nil {
					return nil, err
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE learning_unit_publications SET active=false WHERE unit_id=$1`, u.ID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO learning_unit_publications(unit_id,revision,document_id,run_id,body,question_id,reviewer_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(unit_id,revision) DO UPDATE SET active=true`, u.ID, u.Revision, in.Document, in.Run, nullableJSON(u.Body), qid, userID(ctx)); err != nil {
			return nil, err
		}
		if u.Body.Kind != "question" {
			for i, f := range u.Body.Figures {
				hash := security.Digest(string(u.Images[i]))
				if _, err = tx.ExecContext(ctx, `INSERT INTO learning_blobs(hash,data,size,mime) VALUES($1,$2,$3,'image/png') ON CONFLICT DO NOTHING`, hash, u.Images[i], len(u.Images[i])); err != nil {
					return nil, err
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO learning_publication_assets(id,unit_id,revision,blob_hash,alt,source_page,bbox) VALUES($1,$2,$3,$4,$5,$6,$7)`, security.Token(18), u.ID, u.Revision, hash, f.Alt, f.Page, nullableJSON(f.BBox)); err != nil {
					return nil, err
				}
			}
		}

		if _, err = tx.ExecContext(ctx, `UPDATE learning_content_units SET review_status='approved',question_id=$2 WHERE id=$1`, u.ID, qid); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO learning_resource_reviews(document_id,run_id,unit_id,reviewer_id,action,revision,note) VALUES($1,$2,$3,$4,'approve',$5,$6)`, in.Document, in.Run, u.ID, userID(ctx), u.Revision, in.Note); err != nil {
			return nil, err
		}
		published++
	}
	if in.Replace {
		if _, err = tx.ExecContext(ctx, `UPDATE learning_unit_publications SET active=false WHERE document_id=$1 AND run_id<>$2`, in.Document, in.Run); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE learning_questions q SET status='draft' WHERE q.document_id=$1 AND q.source_key LIKE 'v2:%' AND NOT EXISTS(SELECT 1 FROM learning_unit_publications p WHERE p.question_id=q.id AND p.active)`, in.Document); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE learning_documents SET stage='library',published_run_id=$2,updated_at=now() WHERE id=$1`, in.Document, in.Run); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"published": published, "run_id": in.Run}, nil
}
func selectionIDs(units []reviewSelection) []string {
	ids := []string{}
	for _, u := range units {
		ids = append(ids, u.ID)
	}
	return ids
}
func (s *server) returnResource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision     int    `json:"revision"`
		Unit         string `json:"unit_id"`
		UnitRevision int    `json:"unit_revision"`
		Note         string `json:"note"`
	}
	if decode(r, &in) != nil || in.Revision < 1 || len(strings.TrimSpace(in.Note)) < 3 || len(in.Note) > 2000 {
		fail(w, 400, "请填写退回原因和版本")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "return unit", err)
		return
	}
	defer tx.Rollback()
	var rev int
	if tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&rev) != nil || rev != in.Revision {
		fail(w, 409, "资源已改变")
		return
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE learning_content_units SET review_status='rejected',revision=revision+1 WHERE id=$1 AND document_id=$2 AND revision=$3`, in.Unit, r.PathValue("id"), in.UnitRevision)
	if err == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			fail(w, 409, "内容版本已改变")
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_resource_reviews(document_id,unit_id,reviewer_id,action,revision,note) VALUES($1,$2,$3,'return',$4,$5)`, r.PathValue("id"), in.Unit, userID(r.Context()), in.UnitRevision+1, in.Note)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "return unit", err)
		return
	}
	respond(w, 200, map[string]bool{"returned": true})
}
func (s *server) publishedResources(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT d.id,d.title,d.resource_type,COALESCE(d.subject_id,'') subject_id,(SELECT count(*) FROM learning_unit_publications p WHERE p.document_id=d.id AND p.active) units FROM learning_documents d WHERE d.published_run_id IS NOT NULL ORDER BY d.updated_at DESC LIMIT 100)t`).Scan(&raw)
	if err != nil {
		s.dbError(w, "published resources", err)
		return
	}
	respond(w, 200, map[string]any{"resources": raw})
}
func (s *server) publishedResourceDetail(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 1000000 {
		offset = 0
	}
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT row_to_json(t) FROM (SELECT d.id,d.title,COALESCE((SELECT jsonb_agg(jsonb_build_object('unit_id',p.unit_id,'revision',p.revision,'body',jsonb_build_object('title',p.body->'title','unit_kind',p.body->'unit_kind','subject_id',p.body->'subject_id','resource_type',p.body->'resource_type','content',p.body->'content','pages',p.body->'pages','knowledge_ids',p.body->'knowledge_ids','chapter_ids',p.body->'chapter_ids','topics',p.body->'topics','difficulty',p.body->'difficulty','confidence',p.body->'confidence'),'assets',CASE WHEN p.body->>'unit_kind'='question' THEN '[]'::jsonb ELSE COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'alt',a.alt,'source_page',a.source_page)) FROM learning_publication_assets a WHERE a.unit_id=p.unit_id AND a.revision=p.revision),'[]') END) ORDER BY p.published_at,p.unit_id) FROM (SELECT * FROM learning_unit_publications WHERE document_id=d.id AND active ORDER BY published_at,unit_id LIMIT 25 OFFSET $2) p),'[]') units,(SELECT count(*) FROM learning_unit_publications WHERE document_id=d.id AND active) unit_count FROM learning_documents d WHERE d.id=$1 AND d.published_run_id IS NOT NULL)t`, r.PathValue("id"), offset).Scan(&raw)
	if err != nil {
		fail(w, 404, "正式资源不存在")
		return
	}
	respond(w, 200, map[string]any{"resource": raw})
}
func (s *server) publicationAsset(w http.ResponseWriter, r *http.Request) {
	var data []byte
	err := s.db.QueryRowContext(r.Context(), `SELECT b.data FROM learning_publication_assets a JOIN learning_blobs b ON b.hash=a.blob_hash JOIN learning_unit_publications p ON p.unit_id=a.unit_id AND p.revision=a.revision WHERE a.id=$1 AND p.active AND p.body->>'unit_kind'<>'question'`, r.PathValue("asset")).Scan(&data)
	if err != nil {
		fail(w, 404, "图像不存在")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(data)
}
