package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"example.com/els/api/internal/security"
	"github.com/redis/go-redis/v9"
)

type jobLease struct {
	ID               string
	Generation       int
	Resource         string
	ResourceRevision int
}
type jobLeaseKey struct{}

var errStaleJob = errors.New("任务已取消、暂停或资源版本已改变")
var processWorkerID = workerHost() + ":" + security.Token(18)

func workerHost() string { host, _ := os.Hostname(); return host }

// Lock resource first, then lease. Tag edits use the same lock order. A late
// worker can never commit an intermediate page/question after losing its lease.
func (s *server) learningWriteTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	lease, ok := ctx.Value(jobLeaseKey{}).(jobLease)
	if !ok {
		return tx, nil
	}
	if lease.Resource != "" {
		var revision int
		if err = tx.QueryRowContext(ctx, `SELECT revision FROM learning_documents WHERE id=$1 FOR UPDATE`, lease.Resource).Scan(&revision); err != nil || revision != lease.ResourceRevision {
			tx.Rollback()
			return nil, errStaleJob
		}
	}
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT status='running' AND lease_until>now() AND NOT cancel_requested AND NOT pause_requested
 AND EXISTS(SELECT 1 FROM users u WHERE u.id=owner_id AND NOT u.disabled AND NOT u.must_change_password AND (queue='interactive' OR u.role='admin'))
 FROM learning_jobs WHERE id=$1 AND generation=$2 FOR UPDATE`, lease.ID, lease.Generation).Scan(&valid)
	if err != nil || !valid {
		tx.Rollback()
		return nil, errStaleJob
	}
	return tx, nil
}
func (s *server) learningExec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx, err := s.learningWriteTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, args...)
	if err == nil {
		err = tx.Commit()
	}
	return result, err
}
func (s *server) jobProgress(ctx context.Context, progress any) error {
	j, ok := ctx.Value(jobLeaseKey{}).(jobLease)
	if !ok {
		return nil
	}
	result, err := s.db.ExecContext(ctx, `UPDATE learning_jobs SET progress=$3,updated_at=now() WHERE id=$1 AND generation=$2 AND status='running' AND NOT cancel_requested AND NOT pause_requested AND EXISTS(SELECT 1 FROM users u WHERE u.id=owner_id AND NOT u.disabled AND NOT u.must_change_password AND (queue='interactive' OR u.role='admin'))`, j.ID, j.Generation, nullableJSON(progress))
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errStaleJob
	}
	return nil
}
func (s *server) flushJobSignals(ctx context.Context) {
	if s.redis == nil {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.job_id,o.generation FROM learning_job_signals o JOIN learning_jobs j ON j.id=o.job_id WHERE o.delivered_at IS NULL AND j.status IN ('queued','retry_wait') AND j.retry_at<=now() ORDER BY o.created_at LIMIT 20`)
	if err != nil {
		return
	}
	type signal struct {
		ID         string
		Generation int
	}
	signals := []signal{}
	for rows.Next() {
		var x signal
		if rows.Scan(&x.ID, &x.Generation) == nil {
			signals = append(signals, x)
		}
	}
	rows.Close()
	for _, v := range signals {
		call, cancel := context.WithTimeout(ctx, time.Second)
		err = s.redis.XAdd(call, &redis.XAddArgs{Stream: "learning:jobs", MaxLen: 1000, Approx: true, Values: map[string]any{"id": v.ID, "generation": v.Generation}}).Err()
		cancel()
		if err != nil {
			return
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE learning_job_signals SET delivered_at=now() WHERE job_id=$1 AND generation=$2`, v.ID, v.Generation)
	}
}
func (s *server) learningWorkers(ctx context.Context) {
	wake := make(chan struct{}, 8)
	if s.redis != nil {
		go func() {
			cursor := "$"
			for ctx.Err() == nil {
				signals, err := s.redis.XRead(ctx, &redis.XReadArgs{Streams: []string{"learning:jobs", cursor}, Count: 20, Block: 2 * time.Second}).Result()
				if err != nil {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Second):
					}
					continue
				}
				for _, stream := range signals {
					for _, msg := range stream.Messages {
						cursor = msg.ID
						select {
						case wake <- struct{}{}:
						default:
						}
					}
				}
			}
		}()
	}

	for _, queue := range []string{"interactive", "resource", "resource", "resource", "resource", "resource", "resource", "resource", "resource", "download"} {
		go func(queue string) {
			timer := time.NewTicker(2 * time.Second)
			defer timer.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				_ = s.processLearningJobQueue(ctx, queue)
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				case <-wake:
				}
			}
		}(queue)
	}
	go func() {
		timer := time.NewTicker(10 * time.Second)
		defer timer.Stop()
		for {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO learning_worker_heartbeats(id) VALUES($1) ON CONFLICT(id) DO UPDATE SET updated_at=now()`, processWorkerID)
			s.flushJobSignals(ctx)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
func (s *server) processLearningJob(parent context.Context) error {
	return s.processLearningJobQueue(parent, "")
}
func (s *server) processLearningJobQueue(parent context.Context, queue string) error {
	var id, owner, kind, resource string
	var raw []byte
	var generation, resourceRevision, attempts, maximum int
	var pause, cancelled bool
	claim, err := s.db.BeginTx(parent, nil)
	if err != nil {
		return err
	}
	defer claim.Rollback()
	if _, err = claim.ExecContext(parent, `SELECT pg_advisory_xact_lock(783219)`); err != nil {
		return err
	}
	if queue == "resource" {
		var running, maximum int
		err = claim.QueryRowContext(parent, `SELECT (SELECT count(*) FROM learning_jobs WHERE queue='resource' AND status IN ('running','pause_requested','cancel_requested') AND lease_until>now()),(config->>'worker_concurrency')::integer FROM learning_processing_config WHERE id`).Scan(&running, &maximum)
		if err != nil {
			return err
		}
		if running >= maximum {
			return sql.ErrNoRows
		}
	}
	err = claim.QueryRowContext(parent, `UPDATE learning_jobs SET status='running',generation=generation+1,attempts=attempts+1,lease_owner=$2,lease_until=now()+interval '90 seconds',heartbeat_at=now(),updated_at=now()
 WHERE id=(SELECT id FROM learning_jobs WHERE ($1='' OR queue=$1) AND
 ((status IN ('queued','retry_wait') AND retry_at<=now()) OR (status IN ('running','cancel_requested','pause_requested') AND COALESCE(lease_until,updated_at+interval '12 minutes')<now()))
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING id,owner_id,kind,payload,generation,COALESCE(resource_id,''),COALESCE(resource_revision,0),pause_requested,cancel_requested,attempts,max_attempts`, queue, processWorkerID).Scan(&id, &owner, &kind, &raw, &generation, &resource, &resourceRevision, &pause, &cancelled, &attempts, &maximum)
	if err == nil {
		err = claim.Commit()
	}

	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	var user identity
	err = s.db.QueryRowContext(ctx, `SELECT id,role,group_id FROM users WHERE id=$1 AND NOT disabled AND NOT must_change_password`, owner).Scan(&user.ID, &user.Role, &user.GroupID)
	ctx = context.WithValue(ctx, identityKey{}, user)
	ctx = context.WithValue(ctx, jobLeaseKey{}, jobLease{id, generation, resource, resourceRevision})
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeat, stop := context.WithTimeout(context.WithoutCancel(parent), 4*time.Second)
				var p, c bool
				e := s.db.QueryRowContext(heartbeat, `UPDATE learning_jobs SET heartbeat_at=now(),lease_until=now()+interval '90 seconds' WHERE id=$1 AND generation=$2 AND status IN ('running','pause_requested','cancel_requested') RETURNING pause_requested,cancel_requested`, id, generation).Scan(&p, &c)
				stop()
				if e != nil || p || c {
					cancel()
					return
				}
			}
		}
	}()
	var p learningJobPayload
	var result any
	if pause || cancelled {
		err = errStaleJob
	}
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	if err == nil {
		switch kind {
		case "identify", "outline", "ocr", "extract", "word_options", "resource_scan", "resource_classify", "resource_deep", "resource_publish", "resource_migrate", "share_inspect", "share_analyze", "share_download":
			if user.Role != "admin" {
				err = fmt.Errorf("需要管理员权限")
				break
			}
			switch kind {
			case "identify":
				result, err = s.identifyPDF(ctx, p.DocumentID)
			case "outline":
				result, err = s.outlinePDF(ctx, p.DocumentID)
			case "ocr":
				result, err = s.recognizePDF(ctx, p.DocumentID)
			case "extract":
				result, err = s.extractPDF(ctx, p)
			case "word_options":
				result, err = s.generateWordSet(ctx, p)
			default:
				result, err = s.runResourceJob(ctx, kind, raw)
			}
		case "explanation":
			result, err = s.generateExplanation(ctx, p)
		case "grade", "essay":
			result, err = s.gradeLearning(ctx, p, kind)
		default:
			err = fmt.Errorf("未知任务类型")
		}
	}
	status, message := "completed", ""
	if err != nil {
		status = "failed"
		message = err.Error()
		if len(message) > 500 {
			message = "处理失败，请核对配置或原件"
		}
	}
	if err != nil && attempts < maximum {
		if e, ok := err.(upstreamError); ok && (e.status == 429 || e.status >= 500) {
			status = "retry_wait"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = "retry_wait"
		}
	}
	save, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer stop()
	_, saveErr := s.db.ExecContext(save, `UPDATE learning_jobs SET status=CASE WHEN cancel_requested THEN 'cancelled' WHEN pause_requested THEN 'paused' ELSE $3 END,
 result=$4,error=$5,updated_at=now(),lease_until=NULL,lease_owner=NULL,retry_at=now()+($6*interval '5 seconds') WHERE id=$1 AND generation=$2 AND status IN ('running','pause_requested','cancel_requested')`, id, generation, status, nullableJSON(result), message, attempts*attempts)
	return saveErr
}
func (s *server) runResourceJob(ctx context.Context, kind string, raw []byte) (any, error) {
	var p struct {
		Share    string `json:"share_id"`
		Import   string `json:"import_id"`
		Snapshot int    `json:"snapshot"`
	}
	_ = json.Unmarshal(raw, &p)
	switch kind {
	case "share_inspect":
		return s.inspectShareJob(ctx, p.Share)
	case "share_analyze":
		return s.analyzeShareJob(ctx, p.Share, p.Snapshot)
	case "share_download":
		return s.downloadShareJob(ctx, p.Import)
	case "resource_migrate":
		return s.migrateLegacyOriginals(ctx)
	case "resource_scan":
		return s.scanResources(ctx, raw)
	case "resource_classify":
		return s.classifyResource(ctx, raw)
	case "resource_deep":
		return s.deepAnalyzeResource(ctx, raw)
	case "resource_publish":
		return s.publishResource(ctx, raw)
	}
	return nil, fmt.Errorf("该处理策略尚未启用")
}
func (s *server) sharedResourceJob(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT row_to_json(j) FROM (SELECT id,kind,status,error,attempts,generation,progress,checkpoint,control_revision,created_at,updated_at FROM learning_jobs WHERE id=$1 AND queue<>'interactive') j`, r.PathValue("id")).Scan(&raw)
	if err != nil {
		fail(w, 404, "资源任务不存在")
		return
	}
	respond(w, 200, map[string]any{"job": raw})
}
func (s *server) controlResourceJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision int `json:"revision"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "任务控制格式无效")
		return
	}
	action := r.PathValue("action")
	if action != "pause" && action != "resume" && action != "cancel" && action != "retry" {
		fail(w, 400, "不支持该任务动作")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "job control", err)
		return
	}
	defer tx.Rollback()
	var status string
	var revision int
	err = tx.QueryRowContext(r.Context(), `SELECT status,control_revision FROM learning_jobs WHERE id=$1 AND queue<>'interactive' FOR UPDATE`, r.PathValue("id")).Scan(&status, &revision)
	if err != nil {
		fail(w, 404, "资源任务不存在")
		return
	}
	if in.Revision > 0 && in.Revision != revision {
		fail(w, 409, "任务已改变，请刷新")
		return
	}
	switch action {
	case "pause":
		if status != "queued" && status != "running" && status != "retry_wait" {
			fail(w, 409, "任务当前不能暂停")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_jobs SET pause_requested=true,status=CASE WHEN status='running' THEN 'pause_requested' ELSE 'paused' END,control_revision=control_revision+1 WHERE id=$1`, r.PathValue("id"))
	case "cancel":
		if status == "completed" || status == "cancelled" {
			fail(w, 409, "任务已结束")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_jobs SET cancel_requested=true,status=CASE WHEN status IN ('running','pause_requested') THEN 'cancel_requested' ELSE 'cancelled' END,control_revision=control_revision+1 WHERE id=$1`, r.PathValue("id"))
	case "resume", "retry":
		var stale bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_jobs j JOIN learning_documents d ON d.id=j.resource_id WHERE j.id=$1 AND j.resource_revision<>d.revision)`, r.PathValue("id")).Scan(&stale); err != nil || stale {
			fail(w, 409, "资源版本已改变，请创建新的解析任务")
			return
		}
		if status != "paused" && status != "failed" && status != "cancelled" {
			fail(w, 409, "任务当前不能恢复")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE learning_jobs SET status='queued',pause_requested=false,cancel_requested=false,error='',retry_at=now(),generation=generation+1,control_revision=control_revision+1 WHERE id=$1`, r.PathValue("id"))
	}
	if err == nil {
		err = audit(tx, r, "resource_job_"+action, r.PathValue("id"))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "job control", err)
		return
	}
	respond(w, 202, map[string]bool{"accepted": true})
}
func (s *server) resourceRuntime(w http.ResponseWriter, r *http.Request) {
	var jobs json.RawMessage
	var worker bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_object_agg(status,n),'{}') FROM (SELECT status,count(*) n FROM learning_jobs GROUP BY status) j`).Scan(&jobs); err != nil {
		s.dbError(w, "runtime", err)
		return
	}
	_ = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_worker_heartbeats WHERE updated_at>now()-interval '45 seconds')`).Scan(&worker)
	redisOK := false
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		redisOK = s.redis.Ping(ctx).Err() == nil
		cancel()
	}
	respond(w, 200, map[string]any{"worker_online": worker, "redis_online": redisOK, "database_queue_recovery": true, "jobs": jobs})
}
