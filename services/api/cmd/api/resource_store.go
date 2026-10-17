package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

var resourceTypes = []string{"textbook", "textbook_exercise", "grammar_practice", "knowledge_topic", "unit_test", "comprehensive", "official_paper", "mock_paper", "stage_test", "lecture_notes", "other", "mixed"}

func validResourceType(kind string) bool {
	for _, v := range resourceTypes {
		if v == kind {
			return true
		}
	}
	return false
}

type resourceSource struct {
	Kind, Key, Name, Version string
	Metadata                 any
}

func (s *server) storeRawResource(ctx context.Context, title string, primary, answer []byte, source resourceSource) (string, error) {
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 200 || len(primary) > maxPDF || !bytes.HasPrefix(primary, []byte("%PDF-")) {
		return "", fmt.Errorf("需要32MiB以内的有效PDF与标题")
	}
	if len(answer) > 0 && (len(answer) > maxPDF || !bytes.HasPrefix(answer, []byte("%PDF-"))) {
		return "", fmt.Errorf("答案PDF无效")
	}
	qh, ah := security.Digest(string(primary)), ""
	if len(answer) > 0 {
		ah = security.Digest(string(answer))
	}
	key := security.Digest(qh + ":" + ah)
	tx, err := s.learningWriteTx(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for hash, data := range map[string][]byte{qh: primary, ah: answer} {
		if hash == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO learning_blobs(hash,data,size,mime) VALUES($1,$2,$3,'application/pdf') ON CONFLICT DO NOTHING`, hash, data, len(data)); err != nil {
			return "", err
		}
	}
	id := security.Token(18)
	err = tx.QueryRowContext(ctx, `INSERT INTO learning_documents(id,owner_id,title,resource_key,scope_confirmed) VALUES($1,$2,$3,$4,false) ON CONFLICT(resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, id, userID(ctx), strings.TrimSpace(title), key).Scan(&id)
	if err != nil {
		return "", err
	}
	for role, hash := range map[string]string{"primary": qh, "answer": ah} {
		if hash == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO learning_resource_files(document_id,blob_hash,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, hash, role); err != nil {
			return "", err
		}
	}
	if source.Kind == "" {
		source.Kind = "upload"
	}
	if source.Key == "" {
		source.Key = key + ":" + security.Digest(source.Name)
	}
	if source.Name == "" {
		source.Name = title
	}
	if source.Metadata == nil {
		source.Metadata = map[string]any{}
	}
	var sourceDoc string
	err = tx.QueryRowContext(ctx, `INSERT INTO learning_resource_sources(document_id,kind,source_key,original_name,source_version,metadata) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(kind,source_key,source_version) DO UPDATE SET source_key=EXCLUDED.source_key RETURNING document_id`, id, source.Kind, source.Key, source.Name, source.Version, nullableJSON(source.Metadata)).Scan(&sourceDoc)
	if err != nil {
		return "", err
	}
	if sourceDoc != id {
		return "", fmt.Errorf("来源版本已变化，请重新扫描，不覆盖旧文件")
	}
	return id, tx.Commit()
}
func (s *server) documentPDF(ctx context.Context, id, kind string) ([]byte, error) {
	column, role := "question_pdf", "primary"
	if kind == "answer" {
		column, role = "answer_pdf", "answer"
	} else if kind != "question" {
		return nil, fmt.Errorf("页类型无效")
	}
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(b.data,d.`+column+`) FROM learning_documents d LEFT JOIN learning_resource_files f ON f.document_id=d.id AND f.role=$2 LEFT JOIN learning_blobs b ON b.hash=f.blob_hash WHERE d.id=$1`, id, role).Scan(&data)
	return data, err
}
func (s *server) resourceRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/resources", s.listResources)
	m.HandleFunc("GET /api/v1/admin/resources/config", s.resourceConfig)
	m.HandleFunc("PUT /api/v1/admin/resources/config", s.saveResourceConfig)
	m.HandleFunc("POST /api/v1/admin/resources/{id}/classify", func(w http.ResponseWriter, r *http.Request) { s.queueResourceTask(w, r, "resource_classify") })
	m.HandleFunc("POST /api/v1/admin/resources/uploads", s.uploadRawResource)
	m.HandleFunc("POST /api/v1/admin/resources/scans", s.startResourceScan)
	m.HandleFunc("GET /api/v1/admin/resources/terms", s.resourceTerms)
	m.HandleFunc("GET /api/v1/admin/resources/{id}/file", s.originalResource)
	m.HandleFunc("PATCH /api/v1/admin/resources/{id}/tags", s.editResourceTags)
	m.HandleFunc("GET /api/v1/admin/resources/jobs/{id}/status", s.sharedResourceJob)
	m.HandleFunc("POST /api/v1/admin/resources/jobs/{id}/{action}", s.controlResourceJob)
	m.HandleFunc("GET /api/v1/admin/runtime", s.resourceRuntime)
}
func (s *server) uploadRawResource(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxPDF+1024*1024)
	if r.ParseMultipartForm(1024*1024) != nil {
		fail(w, 400, "上传过大或格式无效")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择原始PDF")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPDF+1))
	if err != nil {
		fail(w, 400, "文件读取失败")
		return
	}
	title := r.FormValue("title")
	if title == "" {
		title = header.Filename
	}
	id, err := s.storeRawResource(r.Context(), title, data, nil, resourceSource{Kind: "upload", Name: header.Filename, Metadata: map[string]string{}})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	respond(w, 201, map[string]any{"id": id, "stage": "A", "message": "原件已保存，可开始轻量识别"})
}
func (s *server) originalResource(w http.ResponseWriter, r *http.Request) {
	data, err := s.documentPDF(r.Context(), r.PathValue("id"), "question")
	if err != nil || len(data) == 0 {
		fail(w, 404, "原件不存在")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="resource.pdf"`)
	_, _ = w.Write(data)
}
func (s *server) listResources(w http.ResponseWriter, r *http.Request) {
	stage, subject, kind, query := r.URL.Query().Get("stage"), r.URL.Query().Get("subject"), r.URL.Query().Get("type"), r.URL.Query().Get("q")
	if len(query) > 400 {
		fail(w, 400, "搜索过长")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 1000000 {
		offset = 0
	}
	var resources json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (
 SELECT d.id,d.title,COALESCE(d.subject_id,'') subject_id,d.stage,d.revision,d.resource_type,d.textbook_alignment,d.classification_status,d.classification_revision,d.classification,d.edition,d.volume,d.recognition,d.created_at,
 COALESCE((SELECT jsonb_agg(row_to_json(x)) FROM (SELECT f.role,b.hash,b.size FROM learning_resource_files f JOIN learning_blobs b ON b.hash=f.blob_hash WHERE f.document_id=d.id) x),'[]') files,
 COALESCE((SELECT jsonb_agg(row_to_json(x)) FROM (SELECT kind,original_name,created_at FROM learning_resource_sources WHERE document_id=d.id) x),'[]') sources,
 COALESCE((SELECT jsonb_agg(row_to_json(x)) FROM (SELECT t.id,t.name,t.kind,t.subject_id FROM learning_resource_terms rt JOIN learning_terms t ON t.id=rt.term_id WHERE rt.document_id=d.id) x),'[]') terms,
 COALESCE((SELECT jsonb_agg(row_to_json(x)) FROM (SELECT id,kind,status,error,generation,attempts,progress,checkpoint,control_revision FROM learning_jobs WHERE resource_id=d.id ORDER BY created_at DESC LIMIT 5) x),'[]') jobs
 FROM learning_documents d WHERE ($1='' OR d.stage=$1) AND ($2='' OR d.subject_id=$2 OR EXISTS(SELECT 1 FROM learning_resource_subjects rs WHERE rs.document_id=d.id AND rs.subject_id=$2)) AND ($3='' OR d.resource_type=$3) AND ($4='' OR d.title ILIKE '%'||$4||'%') ORDER BY d.created_at DESC,d.id LIMIT 25 OFFSET $5) t`, stage, subject, kind, query, offset).Scan(&resources)
	if err != nil {
		s.dbError(w, "resource list", err)
		return
	}
	var counts json.RawMessage
	if err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_object_agg(stage,n),'{}') FROM (SELECT stage,count(*) n FROM learning_documents GROUP BY stage) t`).Scan(&counts); err != nil {
		s.dbError(w, "resource counts", err)
		return
	}
	respond(w, 200, map[string]any{"resources": resources, "counts": counts, "offset": offset, "limit": 25, "types": resourceTypes, "features": map[string]bool{"light": true, "deep": true, "shares": true}})
}

type resourceTags struct {
	Revision  int      `json:"revision"`
	Type      string   `json:"resource_type"`
	Subjects  []string `json:"subjects"`
	Topics    []string `json:"topics"`
	Tags      []string `json:"tags"`
	Knowledge []string `json:"knowledge_ids"`
	Edition   string   `json:"edition"`
	Volume    string   `json:"volume"`
	Alignment string   `json:"textbook_alignment"`
	Confirm   bool     `json:"confirm"`
}

func validateResourceTags(in resourceTags) error {
	if !validResourceType(in.Type) || in.Revision < 1 || len(in.Subjects) > 6 || len(in.Topics) > 20 || len(in.Tags) > 30 || len(in.Knowledge) > 100 || len([]rune(in.Edition)) > 80 || len([]rune(in.Volume)) > 80 {
		return fmt.Errorf("分类字段无效")
	}
	if in.Alignment != "yes" && in.Alignment != "no" && in.Alignment != "unknown" {
		return fmt.Errorf("教材同步状态无效")
	}
	for _, subject := range in.Subjects {
		if len(platform.Types(subject)) == 0 {
			return fmt.Errorf("学科无效")
		}
	}
	if in.Confirm && len(in.Subjects) == 0 {
		return fmt.Errorf("请先确认至少一个学科")
	}
	if in.Confirm && in.Alignment == "yes" {
		if len(in.Subjects) != 1 {
			return fmt.Errorf("教材同步资料请确认主学科")
		}
		if err := platform.ValidateCurriculum(in.Subjects[0], in.Edition, in.Volume); err != nil {
			return err
		}
	}
	for _, name := range append(append([]string{}, in.Topics...), in.Tags...) {
		if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 {
			return fmt.Errorf("标签过长或为空")
		}
	}
	return nil
}
func applyResourceTerms(ctx context.Context, tx *sql.Tx, id string, in resourceTags) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM learning_resource_terms WHERE document_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM learning_resource_subjects WHERE document_id=$1`, id); err != nil {
		return err
	}
	var subject any
	if len(in.Subjects) > 0 {
		subject = in.Subjects[0]
	}
	for _, sid := range in.Subjects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_resource_subjects(document_id,subject_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, sid); err != nil {
			return err
		}
	}
	for kind, names := range map[string][]string{"topic": in.Topics, "tag": in.Tags} {
		for _, name := range names {
			term := kind + "-" + security.Digest(fmt.Sprint(subject) + ":" + strings.TrimSpace(name))[:24]
			if _, err := tx.ExecContext(ctx, `INSERT INTO learning_terms(id,subject_id,kind,name) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, term, subject, kind, strings.TrimSpace(name)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO learning_resource_terms(document_id,term_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, term); err != nil {
				return err
			}
		}
	}
	for _, term := range in.Knowledge {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_terms WHERE id=$1 AND kind='knowledge')`, term).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("知识点不存在，请先在知识点配置中建立")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO learning_resource_terms(document_id,term_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, term); err != nil {
			return err
		}
	}
	return nil
}
func (s *server) editResourceTags(w http.ResponseWriter, r *http.Request) {
	var in resourceTags
	if decodeLimit(r, &in, 24000) != nil {
		fail(w, 400, "标签格式无效")
		return
	}
	if err := validateResourceTags(in); err != nil {
		fail(w, 400, err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "resource tags", err)
		return
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(r.Context(), `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&rev); err != nil {
		fail(w, 404, "资源不存在")
		return
	}
	if rev != in.Revision {
		fail(w, 409, "资源已被修改，请刷新")
		return
	}
	var subject any
	if len(in.Subjects) > 0 {
		subject = in.Subjects[0]
	}
	state, stage := "needs_confirmation", "A"
	if in.Confirm {
		state, stage = "confirmed", "B"
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE learning_documents SET subject_id=$2,resource_type=$3,textbook_alignment=$4,edition=$5,volume=$6,classification_status=$7,stage=CASE WHEN published_run_id IS NOT NULL THEN 'library' ELSE $8 END,revision=revision+1,classification_revision=classification_revision+1,updated_at=now() WHERE id=$1`, r.PathValue("id"), subject, in.Type, in.Alignment, in.Edition, in.Volume, state, stage)
	if err == nil {
		err = applyResourceTerms(r.Context(), tx, r.PathValue("id"), in)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_jobs SET cancel_requested=true,status=CASE WHEN status IN ('queued','retry_wait','paused') THEN 'cancelled' ELSE 'cancel_requested' END,generation=generation+1 WHERE resource_id=$1 AND status IN ('queued','running','retry_wait','paused','pause_requested')`, r.PathValue("id"))
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_classifications(id,document_id,resource_revision,cache_key,result,source) VALUES($1,$2,$3,$4,$5,'human')`, security.Token(18), r.PathValue("id"), rev+1, "human:"+security.Token(12), nullableJSON(in))
	}
	if err == nil {
		err = audit(tx, r, "edit_resource_tags", r.PathValue("id"))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "resource classification", err)
		return
	}
	respond(w, 200, map[string]any{"revision": rev + 1, "stage": stage})
}
func (s *server) resourceTerms(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,name,kind,subject_id,parent_id FROM learning_terms ORDER BY kind,name LIMIT 2000) t`).Scan(&raw)
	if err != nil {
		s.dbError(w, "resource terms", err)
		return
	}
	respond(w, 200, map[string]any{"terms": raw})
}
func (s *server) startResourceScan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Directory string `json:"directory"`
		Recursive bool   `json:"recursive"`
	}
	if decode(r, &in) != nil || strings.HasPrefix(in.Directory, "/") || strings.Contains(in.Directory, "\\") || in.Directory != "" && !fs.ValidPath(in.Directory) {
		fail(w, 400, "只能扫描配置根目录中的相对目录")
		return
	}
	if in.Directory == "" {
		in.Directory = "."
	}
	job, err := s.enqueueLearning(r.Context(), "resource_scan", security.Token(18), in)
	if err != nil {
		s.dbError(w, "queue scan", err)
		return
	}
	respond(w, 202, map[string]string{"job_id": job})
}
func (s *server) scanResources(ctx context.Context, payload []byte) (any, error) {
	var in struct {
		Directory string `json:"directory"`
		Recursive bool   `json:"recursive"`
	}
	if json.Unmarshal(payload, &in) != nil {
		return nil, fmt.Errorf("扫描参数无效")
	}
	root, err := os.OpenRoot(localPDFRoot())
	if err != nil {
		return nil, fmt.Errorf("只读资料目录不可访问")
	}
	defer root.Close()
	subtree, err := root.OpenRoot(in.Directory)
	if err != nil {
		return nil, fmt.Errorf("扫描目录超出配置范围或不存在")
	}
	defer subtree.Close()
	processed, skipped := 0, 0
	ids := []string{}
	err = fs.WalkDir(subtree.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			skipped++
			return nil
		}
		if entry.IsDir() {
			if name != "." && (!in.Recursive || strings.Count(name, "/") >= 8) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(path.Ext(name), ".pdf") {
			return nil
		}
		if processed+skipped >= 5000 {
			return fmt.Errorf("扫描达到5000项上限，请分目录扫描")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxPDF || time.Since(info.ModTime()) < 5*time.Second {
			skipped++
			return nil
		}
		f, err := subtree.Open(name)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxPDF+1))
		after, statErr := f.Stat()
		f.Close()
		if err != nil || statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			skipped++
			return nil
		}
		relative := path.Join(in.Directory, name)
		id, err := s.storeRawResource(ctx, path.Base(name), data, nil, resourceSource{Kind: "local", Key: security.Digest(localPDFRoot() + ":" + relative), Name: relative, Version: security.Digest(string(data)), Metadata: map[string]any{"size": info.Size()}})
		if err != nil {
			return err
		}
		processed++
		ids = append(ids, id)
		return s.jobProgress(ctx, map[string]any{"processed": processed, "skipped": skipped, "file": relative})
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"processed": processed, "skipped": skipped, "resource_ids": ids}, nil
}
