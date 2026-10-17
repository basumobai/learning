package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"example.com/els/api/internal/security"
)

func (s *server) learningCloud(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(row_to_json(t)),'[]') FROM (SELECT id,name,base_url,username,enabled,adapter,options FROM learning_cloud_connections ORDER BY name) t`).Scan(&raw)
	if err != nil {
		s.dbError(w, "cloud connections", err)
		return
	}
	respond(w, 200, map[string]any{"connections": raw})
}
func (s *server) saveLearningCloud(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Secret   string `json:"secret"`
		Enabled  bool   `json:"enabled"`
		Password string `json:"admin_password"`
		Adapter  string `json:"adapter"`
		Options  struct {
			Root  string   `json:"root"`
			Hosts []string `json:"download_hosts"`
		} `json:"options"`
	}
	if decodeLimit(r, &in, 16000) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || len(in.Username) > 300 || len(in.Secret) > 4000 || len(in.ID) > 64 {
		fail(w, 400, "云盘连接无效")
		return
	}
	if in.Adapter == "" {
		in.Adapter = "webdav"
		if in.ID != "" {
			var options []byte
			if s.db.QueryRowContext(r.Context(), `SELECT adapter,options FROM learning_cloud_connections WHERE id=$1`, in.ID).Scan(&in.Adapter, &options) == nil {
				_ = json.Unmarshal(options, &in.Options)
			}
		}
	}
	if in.Adapter != "webdav" && in.Adapter != "openlist" {
		fail(w, 400, "连接适配器无效")
		return
	}
	if in.Options.Root == "" {
		in.Options.Root = "/"
	}
	if !safeMount(in.Options.Root) || len(in.Options.Hosts) > 20 {
		fail(w, 400, "连接目录无效")
		return
	}
	for _, h := range in.Options.Hosts {
		if len(h) > 250 || strings.ContainsAny(h, "/:?@# ") || !strings.Contains(h, ".") {
			fail(w, 400, "下载主机白名单无效")
			return
		}
	}
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if err := validateProviderURL(in.BaseURL); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if !s.adminSession(w, r) {
		return
	}
	if in.ID == "" {
		in.ID = security.Token(18)
	}
	var cipher []byte
	var err error
	if in.Secret != "" {
		cipher, err = security.Seal(s.encryptionKey, in.Secret, "cloud:"+in.ID)
	} else {
		err = s.db.QueryRowContext(r.Context(), `SELECT password FROM learning_cloud_connections WHERE id=$1`, in.ID).Scan(&cipher)
	}
	if err != nil {
		fail(w, 400, "新连接需要凭据，服务器需要加密密钥")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "cloud transaction", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO learning_cloud_connections(id,name,base_url,username,password,enabled,adapter,options) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=$2,base_url=$3,username=$4,password=$5,enabled=$6,adapter=$7,options=$8`, in.ID, in.Name, in.BaseURL, in.Username, cipher, in.Enabled, in.Adapter, nullableJSON(in.Options))
	if err == nil {
		err = audit(tx, r, "save_learning_cloud", in.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "save cloud", err)
		return
	}
	respond(w, 200, map[string]string{"id": in.ID})
}
func cloudResource(base, path string) (string, error) {
	if strings.ContainsAny(path, "\\?#%") || strings.Contains(path, "://") {
		return "", fmt.Errorf("云盘路径无效")
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	escaped := []string{}
	for _, v := range parts {
		if v == "." || v == ".." {
			return "", fmt.Errorf("路径不能跳出配置的目录")
		}
		if v != "" {
			escaped = append(escaped, url.PathEscape(v))
		}
	}
	return strings.TrimRight(base, "/") + "/" + strings.Join(escaped, "/"), nil
}
func (s *server) cloudRequest(ctx context.Context, id, method, path string) (*http.Response, error) {
	var base, user string
	var cipher []byte
	if err := s.db.QueryRowContext(ctx, `SELECT base_url,username,password FROM learning_cloud_connections WHERE id=$1 AND enabled AND adapter='webdav'`, id).Scan(&base, &user, &cipher); err != nil {
		return nil, err
	}
	if err := validateProviderURL(base); err != nil {
		return nil, err
	}
	secret, err := security.Open(s.encryptionKey, cipher, "cloud:"+id)
	if err != nil {
		return nil, err
	}
	target, err := cloudResource(base, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, secret)
	if method == "PROPFIND" {
		req.Header.Set("Depth", "1")
	}
	client := s.client
	if client == nil {
		client = newProviderClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("云盘连接失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("云盘拒绝读取，请检查凭据与路径")
	}
	return resp, nil
}
func (s *server) listLearningCloud(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "目录参数无效")
		return
	}
	resp, err := s.cloudRequest(r.Context(), in.ID, "PROPFIND", in.Path)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	defer resp.Body.Close()
	var out struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Prop struct {
					Display string `xml:"displayname"`
					Size    int64  `xml:"getcontentlength"`
					Type    struct {
						Collection *struct{} `xml:"collection"`
					} `xml:"resourcetype"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if xml.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&out) != nil {
		fail(w, 502, "云盘目录响应无效")
		return
	}
	respond(w, 200, map[string]any{"entries": out.Responses})
}
func (s *server) importCloudPDF(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID           string `json:"connection_id"`
		QuestionPath string `json:"question_path"`
		AnswerPath   string `json:"answer_path"`
		Title        string `json:"title"`
		Subject      string `json:"subject_id"`
		Textbook     string `json:"textbook"`
		Term         string `json:"term"`
		Password     string `json:"admin_password"`
	}
	if decodeLimit(r, &in, 12000) != nil {
		fail(w, 400, "云盘文件参数无效")
		return
	}
	if !s.adminSession(w, r) {
		return
	}
	data := [][]byte{}
	for _, path := range []string{in.QuestionPath, in.AnswerPath} {
		resp, err := s.cloudRequest(r.Context(), in.ID, "GET", path)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		raw, e := io.ReadAll(io.LimitReader(resp.Body, maxPDF+1))
		resp.Body.Close()
		if e != nil || len(raw) > maxPDF || !strings.HasPrefix(string(raw), "%PDF-") {
			fail(w, 400, "云盘PDF无效或超过32MB")
			return
		}
		data = append(data, raw)
	}
	s.storeLearningDocument(w, r, in.Title, in.Subject, in.Textbook, in.Term, data[0], data[1])
}
