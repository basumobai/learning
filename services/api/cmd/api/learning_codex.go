package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// This is a dedicated administrator-configured service, never a user-supplied URL.
func (s *server) codexCompletion(ctx context.Context, model, prompt string, content any) (string, error) {
	target := os.Getenv("LEARNING_CODEX_RUNNER_URL")
	token := os.Getenv("LEARNING_CODEX_RUNNER_TOKEN")
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(token) < 32 {
		return "", fmt.Errorf("管理员需先配置独立Codex入库服务")
	}
	raw, err := json.Marshal(map[string]any{"model": model, "prompt": prompt, "content": content})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(target, "/")+"/generate", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 9 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect disabled") }}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Codex入库服务不可用")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("Codex入库失败，请检查独立服务日志和授权")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return "", fmt.Errorf("Codex输出过大")
	}
	var out struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &out) != nil || out.Text == "" {
		return "", fmt.Errorf("Codex输出无效")
	}
	return out.Text, nil
}
