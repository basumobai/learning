package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"example.com/els/api/internal/security"
)

type catalogModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Context      *int64 `json:"context_length"`
	Architecture struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing    json.RawMessage `json:"pricing"`
	Parameters []string        `json:"supported_parameters"`
}

func (s *server) modelCatalogRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/admin/ai/providers/{id}/models", s.providerCatalog)
	m.HandleFunc("POST /api/v1/admin/ai/providers/{id}/models/sync", s.syncProviderCatalog)
	m.HandleFunc("POST /api/v1/admin/ai/providers/{id}/test", s.syncProviderCatalog)
	m.HandleFunc("PUT /api/v1/admin/ai/model-selections", s.selectCatalogModels)
	m.HandleFunc("PATCH /api/v1/admin/ai/models", s.bulkModelState)
	m.HandleFunc("POST /api/v1/admin/ai/models/{id}/test", s.testConfiguredModel)
	m.HandleFunc("GET /api/v1/admin/ai/task-routes", s.learningTasks)
	m.HandleFunc("PUT /api/v1/admin/ai/task-routes", s.saveLearningTask)
}

func (s *server) providerConnection(ctx context.Context, id string) (aiModel, error) {
	var m aiModel
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT base_url,api_key FROM ai_providers WHERE id=$1 AND enabled`, id).Scan(&m.baseURL, &secret)
	if err != nil {
		return m, fmt.Errorf("服务商不存在或已停用")
	}
	m.key, err = security.Open(s.encryptionKey, secret, "provider:"+id)
	return m, err
}

// Credentials only go to the configured origin. A returned pagination URL cannot
// redirect the authenticated client to a different host or an arbitrary path.
func catalogNext(base, current, next string) (string, error) {
	if next == "" {
		return "", nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	c, err := url.Parse(current)
	if err != nil {
		return "", err
	}
	n, err := url.Parse(next)
	if err != nil {
		return "", err
	}
	n = c.ResolveReference(n)
	if n.Scheme != b.Scheme || n.Host != b.Host || n.Path != b.Path || n.User != nil || n.Fragment != "" {
		return "", fmt.Errorf("模型分页地址不安全")
	}
	return n.String(), nil
}
func (s *server) discoverModels(ctx context.Context, m aiModel) ([]catalogModel, error) {
	if err := validateProviderURL(m.baseURL); err != nil {
		return nil, err
	}
	start := strings.TrimRight(m.baseURL, "/") + "/models"
	target := start
	seen := map[string]bool{}
	result := []catalogModel{}
	ids := map[string]bool{}
	for page := 0; target != ""; page++ {
		if page >= 20 || seen[target] {
			return nil, fmt.Errorf("模型目录分页过多或循环")
		}
		seen[target] = true
		req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+m.key)
		client := s.client
		if client == nil {
			client = newProviderClient()
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("模型目录连接失败")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, upstreamError{response.StatusCode}
		}
		if readErr != nil || len(raw) > 8*1024*1024 {
			return nil, fmt.Errorf("模型目录响应过大")
		}
		var payload struct {
			Data  []catalogModel `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if json.Unmarshal(raw, &payload) != nil || payload.Data == nil {
			return nil, fmt.Errorf("服务商未返回兼容的模型目录；可手动添加")
		}
		for _, item := range payload.Data {
			if !modelSlug.MatchString(item.ID) || ids[item.ID] {
				continue
			}
			ids[item.ID] = true
			if len(result) >= 10000 {
				return nil, fmt.Errorf("模型目录超过10000项")
			}
			if item.Name == "" {
				item.Name = item.ID
			}
			if len([]rune(item.Name)) > 200 {
				item.Name = string([]rune(item.Name)[:200])
			}
			if item.Context != nil && (*item.Context < 1 || *item.Context > 100000000) {
				item.Context = nil
			}
			result = append(result, item)
		}
		target, err = catalogNext(start, target, payload.Links.Next)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
func nullableJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return string(b)
}
func (s *server) syncProviderCatalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	m, err := s.providerConnection(ctx, r.PathValue("id"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	items, err := s.discoverModels(ctx, m)
	if err != nil {
		s.db.ExecContext(ctx, `UPDATE ai_providers SET model_sync_error='目录获取失败；检查连接、鉴权或使用手动添加' WHERE id=$1`, r.PathValue("id"))
		if e, ok := err.(upstreamError); ok && (e.status == 404 || e.status == 405) {
			fail(w, 422, "服务商不支持模型列表，可使用手动添加")
			return
		}
		providerFailure(w, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.dbError(w, "catalog transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT id FROM ai_providers WHERE id=$1 FOR UPDATE`, r.PathValue("id")); err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE ai_model_catalog SET available=false WHERE provider_id=$1`, r.PathValue("id"))
	}
	source := strings.TrimRight(m.baseURL, "/") + "/models"
	unit := ""
	u, _ := url.Parse(m.baseURL)
	if strings.EqualFold(u.Hostname(), "openrouter.ai") {
		unit = "USD/token"
	}
	for _, v := range items {
		if err != nil {
			break
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_model_catalog(provider_id,model_id,name,context_length,input_modalities,output_modalities,pricing,price_unit,supported_parameters,source_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(provider_id,model_id) DO UPDATE SET name=EXCLUDED.name,context_length=EXCLUDED.context_length,input_modalities=EXCLUDED.input_modalities,output_modalities=EXCLUDED.output_modalities,pricing=EXCLUDED.pricing,price_unit=EXCLUDED.price_unit,supported_parameters=EXCLUDED.supported_parameters,source_url=EXCLUDED.source_url,fetched_at=now(),available=true`, r.PathValue("id"), v.ID, v.Name, v.Context, nullableJSON(v.Architecture.Input), nullableJSON(v.Architecture.Output), nullableJSON(v.Pricing), unit, nullableJSON(v.Parameters), source)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE ai_providers SET model_sync_at=now(),model_sync_error='' WHERE id=$1`, r.PathValue("id"))
	}
	if err == nil {
		err = audit(tx, r, "sync_model_catalog", r.PathValue("id"))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "sync catalog", err)
		return
	}
	respond(w, 200, map[string]any{"count": len(items), "message": "目录已同步；目录可见不代表所有模型均可调用"})
}
func (s *server) providerCatalog(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT c.*,m.id AS selected_id,m.enabled AS selected_enabled FROM ai_model_catalog c LEFT JOIN ai_models m ON m.provider_id=c.provider_id AND m.model_id=c.model_id AND m.kind='chat' WHERE c.provider_id=$1 ORDER BY c.name,c.model_id) t`, r.PathValue("id")).Scan(&raw)
	if err != nil {
		s.dbError(w, "catalog", err)
		return
	}
	respond(w, 200, map[string]any{"models": raw})
}
func (s *server) selectCatalogModels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider string   `json:"provider_id"`
		Models   []string `json:"model_ids"`
		Kind     string   `json:"kind"`
		Voices   []string `json:"voices"`
	}
	if decodeLimit(r, &in, 256000) != nil || len(in.Models) == 0 || len(in.Models) > 500 {
		fail(w, 400, "请选择1至500个模型")
		return
	}
	if in.Voices == nil {
		in.Voices = []string{}
	}
	if in.Kind == "" {
		in.Kind = "chat"
	}
	if in.Kind != "chat" && in.Kind != "tts" && in.Kind != "asr" {
		fail(w, 400, "模型用途无效")
		return
	}
	if in.Kind == "tts" && (len(in.Voices) == 0 || len(in.Voices) > 100) {
		fail(w, 400, "语音模型请先填写服务商支持的音色")
		return
	}
	for _, v := range in.Voices {
		if strings.TrimSpace(v) == "" || len(v) > 120 || strings.ContainsAny(v, "\r\n") {
			fail(w, 400, "音色无效")
			return
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "model selection", err)
		return
	}
	defer tx.Rollback()
	ids := []int64{}
	seen := map[string]bool{}
	for _, id := range in.Models {
		if seen[id] {
			continue
		}
		seen[id] = true
		var saved int64
		err = tx.QueryRowContext(r.Context(), `INSERT INTO ai_models(provider_id,model_id,name,kind,voices) SELECT provider_id,model_id,left(name,100),$3,$4 FROM ai_model_catalog WHERE provider_id=$1 AND model_id=$2 AND available ON CONFLICT(provider_id,model_id,kind) DO UPDATE SET enabled=true RETURNING id`, in.Provider, id, in.Kind, nullableJSON(in.Voices)).Scan(&saved)
		if err != nil {
			fail(w, 400, "目录已变化，请刷新后重选")
			return
		}
		ids = append(ids, saved)
	}
	if err = audit(tx, r, "select_catalog_models", in.Provider); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "select models", err)
		return
	}
	respond(w, 200, map[string]any{"model_ids": ids})
}
func (s *server) bulkModelState(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs     []int64 `json:"ids"`
		Enabled bool    `json:"enabled"`
	}
	if decodeLimit(r, &in, 16000) != nil || len(in.IDs) < 1 || len(in.IDs) > 500 {
		fail(w, 400, "请选择模型")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "bulk models", err)
		return
	}
	defer tx.Rollback()
	for _, id := range in.IDs {
		var n sql.Result
		n, err = tx.ExecContext(r.Context(), `UPDATE ai_models SET enabled=$2 WHERE id=$1`, id, in.Enabled)
		if err != nil {
			break
		}
		c, _ := n.RowsAffected()
		if c != 1 {
			fail(w, 409, "模型不存在，请刷新")
			return
		}
	}
	if err == nil {
		err = audit(tx, r, "bulk_model_state", fmt.Sprint(in.IDs))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "bulk state", err)
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (s *server) testConfiguredModel(w http.ResponseWriter, r *http.Request) {
	var id int64
	if _, err := fmt.Sscan(r.PathValue("id"), &id); err != nil {
		fail(w, 400, "模型编号无效")
		return
	}
	m, err := s.learningModel(r.Context(), id)
	if err != nil {
		fail(w, 400, "请选择启用的聊天模型；语音请使用音色试听")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	m.maxOutput = 16
	_, err = s.completion(ctx, m, []chatMessage{{Role: "user", Content: "Reply OK."}}, false)
	if err != nil {
		providerFailure(w, err)
		return
	}
	respond(w, 200, map[string]bool{"available": true})
}
