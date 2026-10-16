package main

import (
	"context"
	"encoding/json"
	"example.com/els/api/internal/platform"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *server) queueLearningOCR(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"admin_password"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "请求格式无效")
		return
	}
	if !s.adminSession(w, r) {
		return
	}
	var exists bool
	if s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_documents WHERE id=$1)`, r.PathValue("id")).Scan(&exists) != nil || !exists {
		fail(w, 404, "资料不存在")
		return
	}
	job, err := s.enqueueLearning(r.Context(), "ocr", r.PathValue("id")+":"+fmt.Sprint(time.Now().UnixNano()), learningJobPayload{DocumentID: r.PathValue("id")})
	if err != nil {
		s.dbError(w, "queue OCR", err)
		return
	}
	respond(w, 202, map[string]string{"job_id": job})
}
func (s *server) learningOCRPages(w http.ResponseWriter, r *http.Request) {
	var pages json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'page',page,'text',text_content,'source',text_source,'notes',ocr_notes) ORDER BY kind,page),'[]') FROM learning_pages WHERE document_id=$1`, r.PathValue("id")).Scan(&pages)
	if err != nil {
		s.dbError(w, "OCR pages", err)
		return
	}
	respond(w, 200, map[string]any{"pages": pages})
}
func (s *server) recognizePDF(ctx context.Context, id string) (any, error) {
	if err := s.preparePDF(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind,page FROM learning_pages WHERE document_id=$1 AND text_source='pending' ORDER BY kind,page`, id)
	if err != nil {
		return nil, err
	}
	type page struct {
		kind string
		n    int
	}
	pages := []page{}
	for rows.Next() {
		var p page
		if err = rows.Scan(&p.kind, &p.n); err != nil {
			rows.Close()
			return nil, err
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return map[string]any{"recognized": 0, "message": "文本已提取，无待识别扫描页"}, nil
	}
	models, _, err := s.taskModels(ctx, "ocr", false)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		img, err := s.pageImage(ctx, id, p.kind, p.n)
		if err != nil {
			return nil, err
		}
		text, err := s.learningCompletion(ctx, models[0], s.taskPrompt(ctx, "ocr")+` 只返回{"text":"逐字识别的文字","uncertain":["模糊位置与需人工核对的原因"]}；不解释、不解题、不补全看不清的文字。`, []map[string]any{textPart(fmt.Sprintf("%s PDF物理页%d", p.kind, p.n)), imagePart(img)})
		if err != nil {
			return nil, err
		}
		var out struct {
			Text      string   `json:"text"`
			Uncertain []string `json:"uncertain"`
		}
		if err = platform.Decode(text, &out); err != nil {
			return nil, err
		}
		if len([]rune(out.Text)) > 24000 || len(out.Uncertain) > 100 || strings.TrimSpace(out.Text) == "" && len(out.Uncertain) == 0 {
			return nil, fmt.Errorf("OCR 未提供文字或不确定说明，请检查视觉模型")
		}
		notes, _ := json.Marshal(out.Uncertain)
		if _, err = s.db.ExecContext(ctx, `UPDATE learning_pages SET text_content=$4,text_source='ocr',ocr_notes=$5 WHERE document_id=$1 AND kind=$2 AND page=$3`, id, p.kind, p.n, out.Text, string(notes)); err != nil {
			return nil, err
		}
	}
	return map[string]any{"recognized": len(pages), "message": "OCR 已完成，请核对页图与文字；资料仍需人工审核"}, nil
}
