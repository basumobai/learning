package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"example.com/els/api/internal/security"
)

func (s *server) adminPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	var hash string
	if err := s.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1 AND role='admin' AND NOT disabled`, userID(r.Context())).Scan(&hash); err != nil {
		s.dbError(w, "admin reauthentication", err)
		return false
	}
	if !security.VerifyPassword(password, hash) {
		fail(w, 403, "请输入正确的管理员当前密码")
		return false
	}
	return true
}
func audit(tx *sql.Tx, r *http.Request, action, target string) error {
	_, err := tx.ExecContext(r.Context(), `INSERT INTO account_audit(actor_id,action,target) VALUES($1,$2,$3)`, userID(r.Context()), action, target)
	return err
}
func (s *server) adminConfig(w http.ResponseWriter, r *http.Request) {
	result := map[string]any{"encryption_ready": len(s.encryptionKey) == 32}
	queries := map[string]string{
		"users":     `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,username,display_name,role,group_id,disabled,must_change_password,totp_secret IS NOT NULL AS mfa_enabled FROM users WHERE username IS NOT NULL ORDER BY username) t`,
		"providers": `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,name,base_url,enabled,true AS has_key FROM ai_providers ORDER BY name) t`,
		"models":    `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,provider_id,model_id,name,kind,voices,instructions,json_mode,enabled FROM ai_models ORDER BY kind,name) t`,
		"groups":    `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT g.id,g.name,g.revision,COALESCE((SELECT jsonb_agg(model_id ORDER BY model_id) FROM group_models WHERE group_id=g.id),'[]') AS model_ids FROM permission_groups g ORDER BY g.id) t`,
		"audit":     `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT a.id,u.username AS actor,a.action,a.target,a.created_at FROM account_audit a LEFT JOIN users u ON u.id=a.actor_id ORDER BY a.id DESC LIMIT 50) t`,
	}
	for name, query := range queries {
		var raw json.RawMessage
		if err := s.db.QueryRowContext(r.Context(), query).Scan(&raw); err != nil {
			s.dbError(w, "admin config", err)
			return
		}
		result[name] = raw
	}
	respond(w, 200, result)
}
func (s *server) saveProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Enabled  bool   `json:"enabled"`
		Password string `json:"admin_password"`
	}
	if decodeLimit(r, &input, 16384) != nil || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 || len(input.APIKey) > 4096 {
		fail(w, 400, "服务商配置无效")
		return
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if err := validateProviderURL(input.BaseURL); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !s.adminSession(w, r) {
		return
	}
	if input.ID == "" {
		input.ID = security.Token(18)
	}
	if len(input.ID) > 64 {
		fail(w, 400, "服务商编号无效")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "provider transaction", err)
		return
	}
	defer tx.Rollback()
	var encrypted []byte
	if input.APIKey != "" {
		encrypted, err = security.Seal(s.encryptionKey, input.APIKey, "provider:"+input.ID)
		if err != nil {
			fail(w, 503, "请先配置服务器 APP_ENCRYPTION_KEY")
			return
		}
	} else {
		err = tx.QueryRowContext(r.Context(), `SELECT api_key FROM ai_providers WHERE id=$1 FOR UPDATE`, input.ID).Scan(&encrypted)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 400, "新服务商需要 API Key")
			return
		}
		if err != nil {
			s.dbError(w, "provider key", err)
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO ai_providers(id,name,base_url,api_key,enabled) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET name=$2,base_url=$3,api_key=$4,enabled=$5,updated_at=now()`, input.ID, strings.TrimSpace(input.Name), input.BaseURL, encrypted, input.Enabled)
	if err == nil {
		err = audit(tx, r, "save_provider", input.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save provider", err)
		return
	}
	respond(w, 200, map[string]string{"id": input.ID})
}

var modelSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/@-]{0,180}$`)

func (s *server) saveModel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID           int64    `json:"id"`
		ProviderID   string   `json:"provider_id"`
		ModelID      string   `json:"model_id"`
		Name         string   `json:"name"`
		Kind         string   `json:"kind"`
		Voices       []string `json:"voices"`
		Instructions string   `json:"instructions"`
		JSONMode     bool     `json:"json_mode"`
		Enabled      bool     `json:"enabled"`
		Password     string   `json:"admin_password"`
	}
	if decodeLimit(r, &input, 16384) != nil || !modelSlug.MatchString(input.ModelID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 || (input.Kind != "chat" && input.Kind != "tts") || len(input.Voices) > 100 || len(input.Instructions) > 1000 {
		fail(w, 400, "模型配置无效")
		return
	}
	if input.Kind == "tts" && len(input.Voices) == 0 {
		fail(w, 400, "语音模型需要至少一个音色")
		return
	}
	for _, v := range input.Voices {
		if strings.TrimSpace(v) == "" || len(v) > 120 || strings.ContainsAny(v, "\n\r") {
			fail(w, 400, "音色标识无效")
			return
		}
	}
	if !s.adminSession(w, r) {
		return
	}
	raw, _ := json.Marshal(input.Voices)
	if input.Voices == nil {
		raw = []byte("[]")
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "model transaction", err)
		return
	}
	defer tx.Rollback()
	var id int64
	if input.ID == 0 {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO ai_models(provider_id,model_id,name,kind,voices,instructions,enabled,json_mode) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider_id,model_id,kind) DO UPDATE SET name=$3,voices=$5,instructions=$6,enabled=$7,json_mode=$8 RETURNING id`, input.ProviderID, input.ModelID, input.Name, input.Kind, string(raw), input.Instructions, input.Enabled, input.JSONMode).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `UPDATE ai_models SET provider_id=$2,model_id=$3,name=$4,kind=$5,voices=$6,instructions=$7,enabled=$8,json_mode=$9 WHERE id=$1 RETURNING id`, input.ID, input.ProviderID, input.ModelID, input.Name, input.Kind, string(raw), input.Instructions, input.Enabled, input.JSONMode).Scan(&id)
	}
	if err != nil {
		fail(w, 400, "服务商或模型配置有误，请检查编号和重复项")
		return
	}
	if err = audit(tx, r, "save_model", input.ModelID); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save model", err)
		return
	}
	respond(w, 200, map[string]int64{"id": id})
}
func (s *server) saveGroup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       int64   `json:"id"`
		Name     string  `json:"name"`
		Models   []int64 `json:"model_ids"`
		Password string  `json:"admin_password"`
		Revision *int    `json:"revision"`
	}
	if decodeLimit(r, &input, 65536) != nil || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 {
		fail(w, 400, "权限组无效")
		return
	}
	if !s.adminSession(w, r) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "group transaction", err)
		return
	}
	defer tx.Rollback()
	id := input.ID
	if id == 0 {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO permission_groups(name) VALUES($1) RETURNING id`, input.Name).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `UPDATE permission_groups SET name=$2,revision=revision+1 WHERE id=$1 AND ($3::integer IS NULL OR revision=$3) RETURNING id`, id, input.Name, input.Revision).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) && input.Revision != nil {
		fail(w, 409, "身份组已被修改，请刷新后重试")
		return
	}
	if err != nil {
		fail(w, 400, "权限组不存在或名称重复")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM group_models WHERE group_id=$1`, id); err != nil {
		s.dbError(w, "group reset", err)
		return
	}
	for _, model := range input.Models {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO group_models(group_id,model_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, model); err != nil {
			fail(w, 400, "所选模型不存在")
			return
		}
	}
	if err = audit(tx, r, "save_group", input.Name); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save group", err)
		return
	}
	respond(w, 200, map[string]int64{"id": id})
}
func (s *server) saveUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Name          string `json:"display_name"`
		Role          string `json:"role"`
		Group         int64  `json:"group_id"`
		Disabled      bool   `json:"disabled"`
		Password      string `json:"password"`
		ClearMFA      bool   `json:"clear_mfa"`
		AdminPassword string `json:"admin_password"`
	}
	input.Role = "learner"
	if decodeLimit(r, &input, 16384) != nil {
		fail(w, 400, "用户信息无效")
		return
	}
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	if !usernamePattern.MatchString(input.Username) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 || (input.Role != "admin" && input.Role != "learner") || input.Group < 1 || (input.Password != "" && !security.PasswordValid(input.Password)) || (input.ID == "" && input.Password == "") || (input.ClearMFA && input.Password == "") {
		fail(w, 400, "检查用户名、权限组及密码（15–128 个字符）；重置两步验证必须同时重置密码")
		return
	}
	if !s.adminPassword(w, r, input.AdminPassword) {
		return
	}
	if input.ID == userID(r.Context()) && (input.Disabled || input.Role != "admin" || input.Password != "" || input.ClearMFA) {
		fail(w, 400, "请在账户安全页修改自己的密码；不能停用或降级当前账户")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "user transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(17283940)`); err != nil {
		s.dbError(w, "admin lock", err)
		return
	}
	hash := ""
	if input.Password != "" {
		hash = security.HashPassword(input.Password)
	}
	if input.ID == "" {
		input.ID = security.Token(18)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO users(id,username,display_name,role,group_id,disabled,password_hash,must_change_password) VALUES($1,$2,$3,$4,$5,$6,$7,true)`, input.ID, input.Username, input.Name, input.Role, input.Group, input.Disabled, hash)
	} else {
		var oldRole string
		if err = tx.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id=$1 AND username IS NOT NULL FOR UPDATE`, input.ID).Scan(&oldRole); err != nil {
			fail(w, 404, "用户不存在")
			return
		}
		if oldRole == "admin" && (input.Disabled || input.Role != "admin") {
			var count int
			if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE role='admin' AND NOT disabled AND id<>$1`, input.ID).Scan(&count); err != nil {
				s.dbError(w, "admin count", err)
				return
			}
			if count == 0 {
				fail(w, 400, "至少保留一名启用的管理员")
				return
			}
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET username=$2,display_name=$3,role=$4,group_id=$5,disabled=$6,
 password_hash=CASE WHEN $7='' THEN password_hash ELSE $7 END,must_change_password=CASE WHEN $7='' THEN must_change_password ELSE true END,
 totp_secret=CASE WHEN $8 THEN NULL ELSE totp_secret END,totp_last_step=CASE WHEN $8 THEN -1 ELSE totp_last_step END,
 totp_pending=CASE WHEN $8 THEN NULL ELSE totp_pending END,totp_pending_expires=CASE WHEN $8 THEN NULL ELSE totp_pending_expires END WHERE id=$1`, input.ID, input.Username, input.Name, input.Role, input.Group, input.Disabled, hash, input.ClearMFA)
		if err == nil && (input.Password != "" || input.Disabled || input.Role != oldRole) {
			_, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, input.ID)
		}
	}
	if err != nil {
		fail(w, 400, "用户名已被使用或权限组不存在")
		return
	}
	if err = audit(tx, r, "save_user", input.Username); err == nil && input.ClearMFA {
		err = audit(tx, r, "reset_mfa", input.Username)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save user", err)
		return
	}
	respond(w, 200, map[string]string{"id": input.ID})
}
