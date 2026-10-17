package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"example.com/els/api/internal/security"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func (s *server) resourceOperationRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/resource-jobs", s.resourceJobs)
	m.HandleFunc("POST /api/v1/admin/resources/tags/batch", s.batchResourceTags)
	m.HandleFunc("POST /api/v1/admin/resources/migrate-originals", func(w http.ResponseWriter, r *http.Request) {
		var in struct{}
		if decode(r, &in) != nil {
			fail(w, 400, "请求格式无效")
			return
		}
		id, err := s.enqueueLearning(r.Context(), "resource_migrate", security.Token(18), map[string]any{})
		if err != nil {
			s.dbError(w, "legacy originals", err)
			return
		}
		respond(w, 202, map[string]string{"job_id": id})
	})
}
func (s *server) resourceJobs(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 1000000 {
		offset = 0
	}
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,resource_id,kind,status,error,progress,checkpoint,generation,attempts,max_attempts,control_revision,ai_token_reserved,ai_cost_reserved,created_at,updated_at FROM learning_jobs WHERE queue<>'interactive' AND ($1='' OR status=$1) ORDER BY created_at DESC,id LIMIT 50 OFFSET $2)t`, r.URL.Query().Get("status"), offset).Scan(&raw)
	if err != nil {
		s.dbError(w, "resource jobs", err)
		return
	}
	respond(w, 200, map[string]any{"jobs": raw, "offset": offset})
}
func (s *server) batchResourceTags(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Resources []reviewSelection `json:"resources"`
		Kind      string            `json:"kind"`
		Names     []string          `json:"names"`
		Remove    bool              `json:"remove"`
	}
	if decodeLimit(r, &in, 20000) != nil || len(in.Resources) < 1 || len(in.Resources) > 100 || in.Kind != "tag" && in.Kind != "topic" || len(in.Names) < 1 || len(in.Names) > 20 {
		fail(w, 400, "请选择资源及专题/辅助标签")
		return
	}
	for _, name := range in.Names {
		if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 {
			fail(w, 400, "标签名称无效")
			return
		}
	}
	sort.Slice(in.Resources, func(i, j int) bool { return in.Resources[i].ID < in.Resources[j].ID })
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "batch tags", err)
		return
	}
	defer tx.Rollback()
	previous := ""
	for _, v := range in.Resources {
		if previous == v.ID {
			fail(w, 400, "重复选择资源")
			return
		}
		previous = v.ID
		var revision int
		var subject string
		if tx.QueryRowContext(r.Context(), `SELECT revision,COALESCE(subject_id,'') FROM learning_documents WHERE id=$1 FOR UPDATE`, v.ID).Scan(&revision, &subject) != nil {
			fail(w, 404, "资源不存在")
			return
		}
		if revision != v.Revision {
			fail(w, 409, "部分资源已修改，请刷新后批量操作")
			return
		}
		if in.Remove {
			_, err = tx.ExecContext(r.Context(), `DELETE FROM learning_resource_terms rt USING learning_terms t WHERE rt.document_id=$1 AND rt.term_id=t.id AND t.kind=$2 AND t.name=ANY($3::text[])`, v.ID, in.Kind, in.Names)
		} else {
			var sid any
			if subject != "" {
				sid = subject
			}
			for _, name := range in.Names {
				id := in.Kind + "-" + security.Digest(fmt.Sprint(sid) + ":" + strings.TrimSpace(name))[:24]
				_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_terms(id,subject_id,kind,name) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, sid, in.Kind, strings.TrimSpace(name))
				if err != nil {
					break
				}
				_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_resource_terms(document_id,term_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, v.ID, id)
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE learning_documents SET revision=revision+1,updated_at=now(),stage=CASE WHEN published_run_id IS NOT NULL THEN 'library' WHEN classification_status IN ('confirmed','proposed') THEN 'B' ELSE 'A' END WHERE id=$1`, v.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE learning_jobs SET cancel_requested=true,status=CASE WHEN status IN ('queued','retry_wait','paused') THEN 'cancelled' ELSE 'cancel_requested' END,generation=generation+1 WHERE resource_id=$1 AND status IN ('queued','running','retry_wait','paused','pause_requested')`, v.ID)
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = audit(tx, r, "batch_resource_tags", strings.Join(selectionIDs(in.Resources), ","))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "batch tags", err)
		return
	}
	respond(w, 200, map[string]int{"updated": len(in.Resources)})
}
func (s *server) migrateLegacyOriginals(ctx context.Context) (any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM learning_documents d WHERE question_pdf IS NOT NULL AND NOT EXISTS(SELECT 1 FROM learning_resource_files f WHERE f.document_id=d.id AND f.role='primary') ORDER BY (published_run_id IS NOT NULL) DESC,created_at,id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	count, duplicates := 0, 0
	for _, id := range ids {
		var q, a []byte
		if err = s.db.QueryRowContext(ctx, `SELECT question_pdf,answer_pdf FROM learning_documents WHERE id=$1`, id).Scan(&q, &a); err != nil {
			return nil, err
		}
		if len(q) == 0 {
			continue
		}
		qh := security.Digest(string(q))
		ah := ""
		if len(a) > 0 {
			ah = security.Digest(string(a))
		}
		key := security.Digest(qh + ":" + ah)
		tx, e := s.learningWriteTx(ctx)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); e != nil {
			tx.Rollback()
			return nil, e
		}
		var canonical string
		e = tx.QueryRowContext(ctx, `SELECT id FROM learning_documents WHERE resource_key=$1`, key).Scan(&canonical)
		if e != nil && e != sql.ErrNoRows {
			tx.Rollback()
			return nil, e
		}
		for hash, data := range map[string][]byte{qh: q, ah: a} {
			if hash == "" {
				continue
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO learning_blobs(hash,data,size,mime) VALUES($1,$2,$3,'application/pdf') ON CONFLICT DO NOTHING`, hash, data, len(data)); e != nil {
				break
			}
		}
		if e == nil {
			for role, hash := range map[string]string{"primary": qh, "answer": ah} {
				if hash == "" {
					continue
				}
				if _, e = tx.ExecContext(ctx, `INSERT INTO learning_resource_files(document_id,blob_hash,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, hash, role); e != nil {
					break
				}
			}
		}
		if e == nil {
			if canonical != "" && canonical != id {
				duplicates++
				_, e = tx.ExecContext(ctx, `UPDATE learning_documents SET duplicate_of=$2 WHERE id=$1`, id, canonical)
			} else {
				_, e = tx.ExecContext(ctx, `UPDATE learning_documents SET resource_key=$2 WHERE id=$1 AND resource_key IS NULL`, id, key)
			}
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `INSERT INTO learning_resource_sources(document_id,kind,source_key,original_name,source_version) SELECT id,'legacy',id,title,$2 FROM learning_documents WHERE id=$1 ON CONFLICT DO NOTHING`, id, key)
		}
		if e == nil {
			e = tx.Commit()
		}
		tx.Rollback()
		if e != nil {
			return nil, e
		}
		count++
		if err = s.jobProgress(ctx, map[string]int{"migrated": count, "duplicates_preserved": duplicates, "batch_limit": 1000}); err != nil {
			return nil, err
		}
	}
	return map[string]any{"migrated": count, "duplicates_preserved": duplicates, "original_columns_preserved": true, "batch_limit": 1000}, nil
}
