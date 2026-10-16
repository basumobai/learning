package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"example.com/els/api/internal/platform"
	"example.com/els/api/internal/security"
)

const maxPDF = 32 * 1024 * 1024

func readPDF(r *http.Request, name string) ([]byte, error) {
	f, _, err := r.FormFile(name)
	if err != nil {
		return nil, fmt.Errorf("请上传题目和答案两个PDF")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPDF+1))
	if err != nil || len(data) > maxPDF || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, fmt.Errorf("PDF无效或超过32MB")
	}
	return data, nil
}
func (s *server) uploadLearningDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxPDF+1024*1024)
	if err := r.ParseMultipartForm(1024 * 1024); err != nil {
		fail(w, 400, "文件过大或上传格式无效")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if !s.adminPassword(w, r, r.FormValue("admin_password")) {
		return
	}
	q, err := readPDF(r, "question_pdf")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	a, err := readPDF(r, "answer_pdf")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.storeLearningDocument(w, r, r.FormValue("title"), r.FormValue("subject_id"), r.FormValue("textbook"), r.FormValue("term"), q, a)
}
func (s *server) storeLearningDocument(w http.ResponseWriter, r *http.Request, title, subject, textbook, term string, q, a []byte) {
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 200 || len(platform.Types(subject)) == 0 || len(textbook) > 200 || len(term) > 100 {
		fail(w, 400, "科目、标题或教材范围无效")
		return
	}
	id := security.Token(18)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO learning_documents(id,owner_id,title,subject_id,textbook,term,question_pdf,answer_pdf) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, userID(r.Context()), title, subject, textbook, term, q, a)
	if err != nil {
		s.dbError(w, "store PDFs", err)
		return
	}
	job, err := s.enqueueLearning(r.Context(), "outline", id, map[string]any{"document_id": id})
	if err != nil {
		s.dbError(w, "queue outline", err)
		return
	}
	respond(w, 202, map[string]string{"id": id, "job_id": job})
}

type chapterSelection struct {
	Key           string `json:"key"`
	Title         string `json:"title"`
	Parent        string `json:"parent"`
	QuestionPages []int  `json:"question_pages"`
	AnswerPages   []int  `json:"answer_pages"`
	Note          string `json:"note"`
}

func (s *server) learningDocuments(w http.ResponseWriter, r *http.Request) {
	var data json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT d.id,d.title,d.subject_id,d.textbook,d.term,d.outline,d.created_at,(SELECT jsonb_agg(jsonb_build_object('id',j.id,'kind',j.kind,'status',j.status,'error',j.error) ORDER BY j.created_at DESC) FROM learning_jobs j WHERE j.owner_id=$1 AND j.payload->>'document_id'=d.id) AS jobs FROM learning_documents d ORDER BY d.created_at DESC LIMIT 100) t`, userID(r.Context())).Scan(&data)
	if err != nil {
		s.dbError(w, "document list", err)
		return
	}
	respond(w, 200, map[string]any{"documents": data})
}
func (s *server) extractLearningDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Chapters []chapterSelection `json:"chapters"`
		Password string             `json:"admin_password"`
	}
	if decodeLimit(r, &in, 64000) != nil || len(in.Chapters) == 0 || len(in.Chapters) > 30 {
		fail(w, 400, "请选择章节及题目/答案页码")
		return
	}
	if !s.adminPassword(w, r, in.Password) {
		return
	}
	if err := s.validateChapterPages(r.Context(), r.PathValue("id"), in.Chapters); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var revision int
	_ = s.db.QueryRowContext(r.Context(), `SELECT revision FROM learning_ai_tasks WHERE task='extract'`).Scan(&revision)
	ids := []string{}
	for _, chapter := range in.Chapters {
		raw, _ := json.Marshal(chapter)
		id, err := s.enqueueLearning(r.Context(), "extract", r.PathValue("id")+":"+security.Digest(string(raw))+":"+strconv.Itoa(revision), map[string]any{"document_id": r.PathValue("id"), "chapters": []chapterSelection{chapter}})
		if err != nil {
			s.dbError(w, "queue extraction", err)
			return
		}
		ids = append(ids, id)
	}
	respond(w, 202, map[string]any{"id": ids[0], "jobs": ids})
}
func (s *server) validateChapterPages(ctx context.Context, document string, chapters []chapterSelection) error {
	keys := map[string]bool{}
	for _, c := range chapters {
		if c.Key == "" || len(c.Key) > 100 || keys[c.Key] || strings.TrimSpace(c.Title) == "" || len(c.QuestionPages) == 0 || len(c.QuestionPages) > 8 || len(c.AnswerPages) == 0 || len(c.AnswerPages) > 8 {
			return fmt.Errorf("每个小节需1至8页题目和1至8页答案；长章节请拆成小节")
		}
		keys[c.Key] = true
		for kind, pages := range map[string][]int{"question": c.QuestionPages, "answer": c.AnswerPages} {
			seen := map[int]bool{}
			for _, p := range pages {
				var ok bool
				if p <= 0 || seen[p] {
					return fmt.Errorf("页码无效或重复")
				}
				seen[p] = true
				if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM learning_pages WHERE document_id=$1 AND kind=$2 AND page=$3)`, document, kind, p).Scan(&ok); err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("%s第%d页不存在，请核对PDF物理页码", kind, p)
				}
			}
		}
	}
	return nil
}
func commandPDF(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("PDF处理失败，请检查文件及Poppler安装")
	}
	return out.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8*1024*1024 {
		return 0, fmt.Errorf("PDF输出过大")
	}
	return b.Buffer.Write(p)
}
func pdfTexts(ctx context.Context, data []byte) ([]string, error) {
	dir, err := os.MkdirTemp("", "learning-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "source.pdf")
	if err = os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	info, err := commandPDF(ctx, "pdfinfo", path)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, l := range strings.Split(string(info), "\n") {
		if strings.HasPrefix(l, "Pages:") {
			count, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, "Pages:")))
		}
	}
	if count < 1 || count > 200 {
		return nil, fmt.Errorf("单份PDF需1至200页，请分册导入")
	}
	raw, err := commandPDF(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(raw), "\f")
	if len(parts) < count {
		return nil, fmt.Errorf("PDF分页无效")
	}
	result := parts[:count]
	for _, p := range result {
		if len([]rune(p)) > 24000 {
			return nil, fmt.Errorf("单页内容过长")
		}
	}
	return result, nil
}
func (s *server) preparePDF(ctx context.Context, id string) error {
	var q, a []byte
	if err := s.db.QueryRowContext(ctx, `SELECT question_pdf,answer_pdf FROM learning_documents WHERE id=$1`, id).Scan(&q, &a); err != nil {
		return err
	}
	qt, err := pdfTexts(ctx, q)
	if err != nil {
		return err
	}
	at, err := pdfTexts(ctx, a)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for kind, parts := range map[string][]string{"question": qt, "answer": at} {
		for i, text := range parts {
			if _, err = tx.ExecContext(ctx, `INSERT INTO learning_pages(document_id,kind,page,text_content,image) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, id, kind, i+1, text, []byte{}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *server) pageImage(ctx context.Context, id, kind string, page int) ([]byte, error) {
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT image FROM learning_pages WHERE document_id=$1 AND kind=$2 AND page=$3`, id, kind, page).Scan(&data); err != nil {
		return nil, err
	}
	if len(data) > 0 {
		return data, nil
	}
	column := "question_pdf"
	if kind == "answer" {
		column = "answer_pdf"
	} else if kind != "question" {
		return nil, fmt.Errorf("页类型无效")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT `+column+` FROM learning_documents WHERE id=$1`, id).Scan(&data); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "learning-page-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "source.pdf")
	if err = os.WriteFile(src, data, 0600); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "page")
	_, err = commandPDF(ctx, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-scale-to", "1400", "-singlefile", "-png", src, out)
	if err != nil {
		return nil, err
	}
	data, err = os.ReadFile(out + ".png")
	if err != nil || len(data) > 8*1024*1024 {
		return nil, fmt.Errorf("PDF渲染无效")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE learning_pages SET image=$4 WHERE document_id=$1 AND kind=$2 AND page=$3`, id, kind, page, data)
	return data, err
}
func (s *server) learningPage(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.PathValue("page"))
	image, err := s.pageImage(r.Context(), r.PathValue("id"), r.PathValue("kind"), page)
	if err != nil {
		fail(w, 404, "无法读取PDF页面")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(image)
}
func imagePart(data []byte) map[string]any {
	return map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)}}
}
func textPart(text string) map[string]any { return map[string]any{"type": "text", "text": text} }
func (s *server) outlinePDF(ctx context.Context, id string) (any, error) {
	models, _, err := s.taskModels(ctx, "outline", false)
	if err != nil {
		return nil, err
	}
	if err = s.preparePDF(ctx, id); err != nil {
		return nil, err
	}
	type pageText struct {
		Page int
		Text string
	}
	pages := []pageText{}
	rows, err := s.db.QueryContext(ctx, `SELECT page,text_content FROM learning_pages WHERE document_id=$1 AND kind='question' ORDER BY page`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p pageText
		if rows.Scan(&p.Page, &p.Text) != nil {
			rows.Close()
			return nil, fmt.Errorf("PDF页读取失败")
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	answerIndex := ""
	rows, err = s.db.QueryContext(ctx, `SELECT page,text_content FROM learning_pages WHERE document_id=$1 AND kind='answer' ORDER BY page`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p int
		var text string
		if rows.Scan(&p, &text) != nil {
			continue
		}
		r := []rune(text)
		answerIndex += fmt.Sprintf("答案物理页%d：%s\n", p, string(r[:min(len(r), 150)]))
	}
	rows.Close()
	all := []chapterSelection{}
	scanned := 0
	var previous []byte
	_ = s.db.QueryRowContext(ctx, `SELECT outline,outline_scanned_pages FROM learning_documents WHERE id=$1`, id).Scan(&previous, &scanned)
	var prior struct {
		Chapters []chapterSelection `json:"chapters"`
	}
	_ = json.Unmarshal(previous, &prior)
	all = prior.Chapters
	if all == nil {
		all = []chapterSelection{}
	}
	for start := scanned; start < len(pages); start += 8 {
		parts := []map[string]any{textPart("答案索引（页码须管理员复核；空白表示扫描页）：\n" + answerIndex)}
		for _, p := range pages[start:min(start+8, len(pages))] {
			parts = append(parts, textPart(fmt.Sprintf("题目物理页%d：\n%s", p.Page, p.Text)))
			if len(strings.TrimSpace(p.Text)) < 80 {
				image, err := s.pageImage(ctx, id, "question", p.Page)
				if err != nil {
					return nil, err
				}
				parts = append(parts, imagePart(image))
			}
		}
		text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, "outline")+` 返回{"chapters":[{"key":"短编号","title":"小节名称","parent":"父章节名称或空","question_pages":[1],"answer_pages":[1],"note":"不确定内容与审核说明"}]}。每小节最多8页题目和8页答案；长章节拆成多个小节。题目页只能取当前提供的物理页。无法确定答案页时answer_pages留空。`, parts)
		if err != nil {
			return nil, err
		}
		var out struct {
			Chapters []chapterSelection `json:"chapters"`
		}
		if err = platform.Decode(text, &out); err != nil {
			return nil, err
		}
		if len(out.Chapters) > 40 {
			return nil, fmt.Errorf("章节过多")
		}
		for i, c := range out.Chapters {
			if strings.TrimSpace(c.Title) == "" || len(c.QuestionPages) == 0 || len(c.QuestionPages) > 8 {
				return nil, fmt.Errorf("章节结构无效")
			}
			for _, p := range c.QuestionPages {
				if p < pages[start].Page || p > pages[min(start+8, len(pages))-1].Page {
					return nil, fmt.Errorf("章节页码越界")
				}
			}
			c.Key = fmt.Sprintf("p%d-%d", start+1, i+1)
			all = append(all, c)
		}
		progress, _ := json.Marshal(map[string]any{"chapters": all, "requires_review": true, "complete": false})
		if _, err = s.db.ExecContext(ctx, `UPDATE learning_documents SET outline=$2,outline_scanned_pages=$3 WHERE id=$1`, id, string(progress), min(start+8, len(pages))); err != nil {
			return nil, err
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("未发现章节，请核对PDF")
	}
	result := map[string]any{"chapters": all, "requires_review": true, "complete": true}
	raw, _ := json.Marshal(result)
	_, err = s.db.ExecContext(ctx, `UPDATE learning_documents SET outline=$2 WHERE id=$1`, id, string(raw))
	return result, err
}

type figureCrop struct {
	Page int        `json:"page"`
	BBox [4]float64 `json:"bbox"`
	Alt  string     `json:"alt"`
}
type extractedQuestion struct {
	SourceKey     string            `json:"source_key"`
	Kind          string            `json:"kind"`
	Content       platform.Content  `json:"content"`
	Solution      platform.Solution `json:"solution"`
	QuestionPages []int             `json:"question_pages"`
	AnswerPages   []int             `json:"answer_pages"`
	AnswerQuote   string            `json:"answer_quote"`
	NeedsReview   bool              `json:"needs_review"`
	Figures       []figureCrop      `json:"figures"`
}

func cropPNG(raw []byte, bbox [4]float64) ([]byte, error) {
	for _, n := range bbox {
		if n < 0 || n > 1 {
			return nil, fmt.Errorf("插图范围越界")
		}
	}
	if bbox[2] <= bbox[0] || bbox[3] <= bbox[1] {
		return nil, fmt.Errorf("插图范围无效")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	rect := image.Rect(int(bbox[0]*float64(b.Dx())), int(bbox[1]*float64(b.Dy())), int(bbox[2]*float64(b.Dx())), int(bbox[3]*float64(b.Dy()))).Add(b.Min)
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok || rect.Dx() < 4 || rect.Dy() < 4 {
		return nil, fmt.Errorf("插图过小")
	}
	var out bytes.Buffer
	err = png.Encode(&out, sub.SubImage(rect))
	return out.Bytes(), err
}
func containsPage(pages []int, p int) bool {
	for _, v := range pages {
		if v == p {
			return true
		}
	}
	return false
}
func (s *server) extractPDF(ctx context.Context, p learningJobPayload) (any, error) {
	if err := s.validateChapterPages(ctx, p.DocumentID, p.Chapters); err != nil {
		return nil, err
	}
	models, _, err := s.taskModels(ctx, "extract", false)
	if err != nil {
		return nil, err
	}
	var subject, textbook, term string
	if err = s.db.QueryRowContext(ctx, `SELECT subject_id,textbook,term FROM learning_documents WHERE id=$1`, p.DocumentID).Scan(&subject, &textbook, &term); err != nil {
		return nil, err
	}
	added := 0
	for _, chapter := range p.Chapters {
		types, _ := json.Marshal(platform.Types(subject))
		parts := []map[string]any{textPart("目标小节：" + chapter.Title + "；科目：" + subject + "；允许题型：" + string(types))}
		for _, kind := range []string{"question", "answer"} {
			pages := chapter.QuestionPages
			if kind == "answer" {
				pages = chapter.AnswerPages
			}
			for _, page := range pages {
				var text string
				if err = s.db.QueryRowContext(ctx, `SELECT text_content FROM learning_pages WHERE document_id=$1 AND kind=$2 AND page=$3`, p.DocumentID, kind, page).Scan(&text); err != nil {
					return nil, err
				}
				img, err := s.pageImage(ctx, p.DocumentID, kind, page)
				if err != nil {
					return nil, err
				}
				parts = append(parts, textPart(fmt.Sprintf("%s物理页%d，OCR/文本：\n%s", kind, page, text)), imagePart(img))
			}
		}
		prompt := s.taskPrompt(ctx, "extract") + ` 返回{"questions":[{"source_key":"PDF原题号，文档内唯一","kind":"允许题型ID","content":{"text":"题干说明","passage":"共用阅读/材料或空","parts":[{"id":"1","text":"小题题干","choices":[{"id":"A","text":"选项"}],"points":1}]},"solution":{"answers":{"1":"A"},"explanation":"结合参考答案的详细讲解","rubric":"评分要点"},"question_pages":[1],"answer_pages":[1],"answer_quote":"从答案页逐字摘录该题原始答案","needs_review":true,"figures":[{"page":1,"bbox":[0.1,0.1,0.8,0.8],"alt":"图的内容说明"}]}]}。选择题答案为选项ID，多选用逗号分隔如A,C；主观题choices留空。插图bbox按题目页渲染图归一化[x0,y0,x1,y1]。只截插图，不把包含答案的页面作为插图。不要动态讲解或动画数据。`
		text, err := s.learningCompletion(ctx, models[0], prompt, parts)
		if err != nil {
			return nil, err
		}
		var out struct {
			Questions []extractedQuestion `json:"questions"`
		}
		if err = platform.Decode(text, &out); err != nil {
			return nil, err
		}
		if len(out.Questions) < 1 || len(out.Questions) > 60 {
			return nil, fmt.Errorf("题目数量无效")
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		chapterID := "chapter-" + security.Digest(p.DocumentID + ":" + chapter.Key)[:24]
		var parent any
		if chapter.Parent != "" {
			pid := "chapter-" + security.Digest(p.DocumentID + ":parent:" + chapter.Parent)[:24]
			if _, err = tx.ExecContext(ctx, `INSERT INTO learning_chapters(id,subject_id,title,textbook,term) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, pid, subject, chapter.Parent, textbook, term); err != nil {
				tx.Rollback()
				return nil, err
			}
			parent = pid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO learning_chapters(id,subject_id,parent_id,title,textbook,term) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, chapterID, subject, parent, chapter.Title, textbook, term)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		for _, q := range out.Questions {
			if err = platform.ValidateQuestion(subject, q.Kind, q.Content, q.Solution); err != nil {
				tx.Rollback()
				return nil, err
			}
			if q.SourceKey == "" || len(q.SourceKey) > 100 || q.AnswerQuote == "" || len(q.QuestionPages) == 0 || len(q.AnswerPages) == 0 || len(q.Figures) > 12 {
				tx.Rollback()
				return nil, fmt.Errorf("题号、原始答案依据或插图无效")
			}
			for kind, pages := range map[string][]int{"question": q.QuestionPages, "answer": q.AnswerPages} {
				allowed := chapter.QuestionPages
				if kind == "answer" {
					allowed = chapter.AnswerPages
				}
				for _, page := range pages {
					if !containsPage(allowed, page) {
						tx.Rollback()
						return nil, fmt.Errorf("来源页码越界")
					}
				}
			}
			content, _ := json.Marshal(q.Content)
			sol, _ := json.Marshal(q.Solution)
			provenance, _ := json.Marshal(map[string]any{"question_pages": q.QuestionPages, "answer_pages": q.AnswerPages, "original_answer": q.AnswerQuote, "needs_review": q.NeedsReview, "ai_generated": true})
			qid := security.Token(18)
			result, err := tx.ExecContext(ctx, `INSERT INTO learning_questions(id,subject_id,chapter_id,kind,content,solution,provenance,document_id,source_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(document_id,source_key) DO NOTHING`, qid, subject, chapterID, q.Kind, string(content), string(sol), string(provenance), p.DocumentID, q.SourceKey)
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			n, _ := result.RowsAffected()
			if n == 0 {
				continue
			}
			for _, f := range q.Figures {
				if !containsPage(q.QuestionPages, f.Page) || f.Alt == "" {
					tx.Rollback()
					return nil, fmt.Errorf("插图来源无效")
				}
				img, err := s.pageImage(ctx, p.DocumentID, "question", f.Page)
				if err != nil {
					tx.Rollback()
					return nil, err
				}
				cropped, err := cropPNG(img, f.BBox)
				if err != nil {
					tx.Rollback()
					return nil, err
				}
				bbox, _ := json.Marshal(f.BBox)
				if _, err = tx.ExecContext(ctx, `INSERT INTO learning_assets(id,question_id,image,alt,source_page,bbox) VALUES($1,$2,$3,$4,$5,$6)`, security.Token(18), qid, cropped, f.Alt, f.Page, string(bbox)); err != nil {
					tx.Rollback()
					return nil, err
				}
			}
			added++
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}
	return map[string]any{"added": added, "status": "draft", "message": "请核对题干、插图、答案和页码后发布"}, nil
}
func (s *server) adminLearningQuestions(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT q.*,(SELECT jsonb_agg(jsonb_build_object('id',a.id,'alt',a.alt,'bbox',a.bbox,'source_page',a.source_page)) FROM learning_assets a WHERE a.question_id=q.id) AS assets FROM learning_questions q WHERE ($1='' OR q.document_id=$1) ORDER BY q.created_at DESC LIMIT 100) t`, r.URL.Query().Get("document_id")).Scan(&raw)
	if err != nil {
		s.dbError(w, "draft questions", err)
		return
	}
	respond(w, 200, map[string]any{"questions": raw})
}
func (s *server) saveLearningQuestion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content  platform.Content  `json:"content"`
		Solution platform.Solution `json:"solution"`
		Status   string            `json:"status"`
		Revision int               `json:"revision"`
		Password string            `json:"admin_password"`
	}
	if decodeLimit(r, &in, 240000) != nil || (in.Status != "draft" && in.Status != "published") {
		fail(w, 400, "题目格式无效")
		return
	}
	if !s.adminPassword(w, r, in.Password) {
		return
	}
	var subject, kind string
	if err := s.db.QueryRowContext(r.Context(), `SELECT subject_id,kind FROM learning_questions WHERE id=$1`, r.PathValue("id")).Scan(&subject, &kind); err != nil {
		fail(w, 404, "题目不存在")
		return
	}
	if err := platform.ValidateQuestion(subject, kind, in.Content, in.Solution); err != nil {
		fail(w, 400, err.Error())
		return
	}
	c, _ := json.Marshal(in.Content)
	sol, _ := json.Marshal(in.Solution)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "edit question", err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE learning_questions SET content=$2,solution=$3,status=$4,revision=revision+1 WHERE id=$1 AND revision=$5`, r.PathValue("id"), string(c), string(sol), in.Status, in.Revision)
	if err == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			fail(w, 409, "题目版本已更新，请刷新后重试")
			return
		}
		err = audit(tx, r, "save_learning_question", r.PathValue("id"))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save question", err)
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
