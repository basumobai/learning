package main

import (
	"bytes"
	"context"
	"encoding/json"
	"example.com/els/api/internal/security"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestShareBoundaries(t *testing.T) {
	for _, raw := range []string{"http://pan.quark.cn/s/abcdef", "https://pan.quark.cn.evil.test/s/abcdef", "https://user@pan.baidu.com/s/abcdef", "https://127.0.0.1/s/abcdef"} {
		if _, _, err := identifyShare(raw); err == nil {
			t.Fatal("unsafe share accepted", raw)
		}
	}
	p, u, err := identifyShare("https://pan.baidu.com/s/123456?pwd=secret#file")
	if err != nil || p != "baidu" || strings.Contains(u, "secret") {
		t.Fatal(p, u, err)
	}
	if safeMount("/foo/../escape") || withinMount("/allowed", "/allowed-other") {
		t.Fatal("mount escape")
	}
	if downloadHostAllowed("https://files.test", "https://evil.test/f?sign=x", nil) || downloadHostAllowed("https://files.test", "https://127.0.0.1/f", []string{"127.0.0.1"}) {
		t.Fatal("download SSRF")
	}
	if !downloadHostAllowed("https://files.test", "https://files.test/p/a?sign=x", nil) {
		t.Fatal("same origin proxy rejected")
	}
}
func TestShareSelectionPrivacyAndImport(t *testing.T) {
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	pdf, _ := os.ReadFile("testdata/questions.pdf")
	secret := "sensitive-openlist-token"
	cipher, _ := security.Seal(s.encryptionKey, secret, "cloud:mounted")
	_, err := s.db.Exec(`INSERT INTO learning_cloud_connections(id,name,base_url,username,password,enabled,adapter,options) VALUES('mounted','Mounted','https://files.test','',$1,true,'openlist','{"root":"/share","download_hosts":[]}')`, cipher)
	if err != nil {
		t.Fatal(err)
	}
	var downloads, paid atomic.Int64
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}
		raw := ""
		switch r.URL.Path {
		case "/api/fs/list":
			if r.Header.Get("Authorization") != secret {
				t.Error("missing connection auth")
			}
			raw = fmt.Sprintf(`{"code":200,"data":{"content":[{"name":"selected.pdf","size":%d,"is_dir":false,"modified":"v1","hash_info":{"sha256":"%s"}},{"name":"unselected.pdf","size":2000,"is_dir":false,"modified":"v1"}],"total":2}}`, len(pdf), security.Digest(string(pdf)))
		case "/api/fs/get":
			if body["path"] != "/share/selected.pdf" {
				t.Error("unselected metadata requested", body)
			}
			raw = fmt.Sprintf(`{"code":200,"data":{"name":"selected.pdf","size":%d,"modified":"v1","hash_info":{"sha256":"%s"},"raw_url":"https://files.test/p/selected.pdf?sign=signed","header":""}}`, len(pdf), security.Digest(string(pdf)))
		case "/p/selected.pdf":
			downloads.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("token leaked to download")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(pdf)), Header: http.Header{}}, nil
		default:
			paid.Add(1)
			t.Error("unexpected upstream", r.URL.Path)
			raw = `{}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
	})}
	input := map[string]any{"url": "https://pan.quark.cn/s/123456", "passcode": "code-private", "connection_id": "mounted", "mount_path": "/share", "authorized_mount": true}
	student.call("POST", "/api/v1/admin/resources/shares/inspect", input, 403)
	out := admin.call("POST", "/api/v1/admin/resources/shares/inspect", input, 202)
	share := rawString(out, "share_id")
	if err = s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw := admin.call("GET", "/api/v1/admin/resources/shares/"+share+"/files", nil, 200)
	var snapshot int
	json.Unmarshal(raw["snapshot"], &snapshot)
	var files []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(raw["files"], &files)
	if snapshot != 2 || len(files) != 2 {
		t.Fatal(raw)
	}
	var chosen string
	for _, f := range files {
		if f.Name == "selected.pdf" {
			chosen = f.ID
		}
	}
	admin.call("POST", "/api/v1/admin/resources/shares/"+share+"/imports", map[string]any{"snapshot": snapshot - 1, "file_ids": []string{chosen}, "max_bytes": len(pdf), "bytes_per_second": 1048576}, 409)
	job := admin.call("POST", "/api/v1/admin/resources/shares/"+share+"/imports", map[string]any{"snapshot": snapshot, "file_ids": []string{chosen}, "max_bytes": len(pdf), "bytes_per_second": 1048576}, 202)
	if err = s.processLearningJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status, message string
	s.db.QueryRow(`SELECT status,error FROM learning_jobs WHERE id=$1`, rawString(job, "job_id")).Scan(&status, &message)
	if status != "completed" || downloads.Load() != 1 || paid.Load() != 0 {
		t.Fatal(status, message, downloads.Load(), paid.Load())
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM learning_documents WHERE stage='A'`).Scan(&count)
	if count != 1 {
		t.Fatal("unselected files imported", count)
	}
	response := admin.raw("GET", "/api/v1/admin/resources/shares", nil, 200)
	if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), "code-private") {
		t.Fatal("credentials exposed")
	}
}
