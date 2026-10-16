package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"example.com/els/api/internal/security"
)

func publicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	return a.IsGlobalUnicast() && !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() && !netip.MustParsePrefix("100.64.0.0/10").Contains(a) && !netip.MustParsePrefix("198.18.0.0/15").Contains(a)
}
func validateProviderURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("接口地址应为公开 HTTPS 基础地址，例如 https://openrouter.ai/api/v1")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") {
		return fmt.Errorf("不允许访问内部接口地址")
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return fmt.Errorf("不允许访问内部接口地址")
	}
	return nil
}
func newProviderClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 100 * time.Second, MaxIdleConns: 50, IdleConnTimeout: 90 * time.Second, ForceAttemptHTTP2: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("provider DNS empty")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, fmt.Errorf("provider DNS includes private address")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("provider redirects are disabled") }}
}

type aiModel struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	ModelID      string   `json:"model_id"`
	Kind         string   `json:"kind"`
	Voices       []string `json:"voices"`
	ProviderName string   `json:"provider"`
	baseURL      string
	key          string
	instructions string
	version      string
	jsonMode     bool
}

func (m aiModel) hasVoice(voice string) bool {
	for _, v := range m.Voices {
		if v == voice {
			return true
		}
	}
	return false
}
func (s *server) allowedModel(ctx context.Context, id int64, kind string) (aiModel, error) {
	m := aiModel{Voices: []string{}}
	var raw, cipher []byte
	var providerID string
	var updated time.Time
	err := s.db.QueryRowContext(ctx, `SELECT m.id,m.name,m.model_id,m.kind,m.voices,p.name,p.base_url,p.api_key,m.instructions,p.updated_at,m.json_mode,m.provider_id FROM ai_models m
 JOIN ai_providers p ON p.id=m.provider_id JOIN group_models gm ON gm.model_id=m.id JOIN users u ON u.group_id=gm.group_id
 WHERE u.id=$1 AND NOT u.disabled AND NOT u.must_change_password AND m.id=$2 AND m.kind=$3 AND m.enabled AND p.enabled`, userID(ctx), id, kind).Scan(&m.ID, &m.Name, &m.ModelID, &m.Kind, &raw, &m.ProviderName, &m.baseURL, &cipher, &m.instructions, &updated, &m.jsonMode, &providerID)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(raw, &m.Voices); err != nil {
		return m, err
	}
	m.key, err = security.Open(s.encryptionKey, cipher, "provider:"+providerID)
	m.version = updated.Format(time.RFC3339Nano)
	return m, err
}
func (s *server) models(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT m.id,m.name,m.model_id,m.kind,m.voices,p.name FROM ai_models m JOIN ai_providers p ON p.id=m.provider_id JOIN group_models gm ON gm.model_id=m.id JOIN users u ON u.group_id=gm.group_id WHERE u.id=$1 AND m.enabled AND p.enabled ORDER BY m.kind,m.name`, userID(r.Context()))
	if err != nil {
		s.dbError(w, "allowed models", err)
		return
	}
	defer rows.Close()
	models := []aiModel{}
	for rows.Next() {
		m := aiModel{Voices: []string{}}
		var raw []byte
		if err = rows.Scan(&m.ID, &m.Name, &m.ModelID, &m.Kind, &raw, &m.ProviderName); err != nil {
			s.dbError(w, "allowed model row", err)
			return
		}
		if err = json.Unmarshal(raw, &m.Voices); err != nil {
			s.dbError(w, "model voices", err)
			return
		}
		models = append(models, m)
	}
	if rows.Err() != nil {
		s.dbError(w, "allowed model rows", rows.Err())
		return
	}
	respond(w, 200, map[string]any{"models": models})
}

type upstreamError struct{ status int }

func (e upstreamError) Error() string { return fmt.Sprintf("provider returned HTTP %d", e.status) }
func (s *server) upstream(ctx context.Context, m aiModel, path string, body any, limit int64) ([]byte, error) {
	if err := validateProviderURL(m.baseURL); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.key)
	req.Header.Set("HTTP-Referer", s.origin)
	req.Header.Set("X-Title", "Learning Study Platform")
	client := s.client
	if client == nil {
		client = newProviderClient()
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, upstreamError{response.StatusCode}
	}
	if path == "/audio/speech" {
		ct := strings.ToLower(strings.Split(response.Header.Get("Content-Type"), ";")[0])
		if ct != "audio/mpeg" && ct != "audio/mp3" && ct != "application/octet-stream" {
			return nil, fmt.Errorf("provider returned non-MP3 content")
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit || len(data) == 0 {
		return nil, fmt.Errorf("provider response invalid or too large")
	}
	return data, nil
}
func providerFailure(w http.ResponseWriter, err error) {
	message := "模型服务暂不可用，请稍后重试或换一个已授权模型"
	if e, ok := err.(upstreamError); ok {
		switch e.status {
		case 401, 403:
			message = "模型服务认证失败，请联系管理员检查 API Key"
		case 402:
			message = "模型供应商账户余额不足，请联系管理员"
		case 404:
			message = "模型未找到，请检查模型标识"
		case 400:
			message = "模型不支持本次参数，请换模型或检查配置"
		case 429:
			message = "模型供应商暂时繁忙，请稍后重试"
		}
	}
	fail(w, 502, message)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *server) completion(ctx context.Context, m aiModel, messages []chatMessage, structured bool) (string, error) {
	body := map[string]any{"model": m.ModelID, "messages": messages, "stream": false}
	if structured && m.jsonMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	raw, err := s.upstream(ctx, m, "/chat/completions", body, 2*1024*1024)
	if err != nil {
		return "", err
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("provider returned no text")
	}
	if result.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("provider response truncated")
	}
	return result.Choices[0].Message.Content, nil
}

// No application call budgets, conversation quotas, or model fallbacks are applied.
// Each request carries exactly the model authorized for the current group.
