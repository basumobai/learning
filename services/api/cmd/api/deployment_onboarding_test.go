package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"example.com/els/api/internal/security"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPSOriginThroughReverseProxy(t *testing.T) {
	t.Setenv("APP_ORIGIN", "https://www.sumobai.com")
	s, err := configure(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.secureCookie {
		t.Fatal("HTTPS origin did not enable Secure cookie")
	}
	for _, test := range []struct {
		origin, forwarded, site string
		want                    int
	}{
		{"https://www.sumobai.com", "http://api:8080", "same-origin", 204},
		{"https://evil.test", "https://www.sumobai.com", "same-origin", 403},
		{"http://www.sumobai.com", "https://www.sumobai.com", "same-origin", 403},
		{"null", "https://www.sumobai.com", "cross-site", 403},
		{"", "https://www.sumobai.com", "cross-site", 403},
	} {
		r := httptest.NewRequest("POST", "http://api:8080/api/v1/auth/login", strings.NewReader("{}"))
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-Forwarded-Host", test.forwarded)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-ELS-Client", "web")
		r.Header.Set("Sec-Fetch-Site", test.site)
		w := httptest.NewRecorder()
		s.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("origin %q got %d: %s", test.origin, w.Code, w.Body)
		}
		if w.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatal("wildcard CORS")
		}
	}
	w := httptest.NewRecorder()
	s.cookie(w, "test-token")
	c := w.Result().Cookies()[0]
	if !strings.HasPrefix(c.Name, "__Host-") || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Domain != "" {
		t.Fatal("unsafe HTTPS session cookie", c)
	}
}
func TestRegistrationAndHTTPSLogin(t *testing.T) {
	db := testDatabase(t)
	t.Setenv("APP_ORIGIN", "https://www.sumobai.com")
	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	s, err := configure(db)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.routes()
	request := func(path string, body any, origin string, want int) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Forwarded-Host", "www.sumobai.com")
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		return w
	}
	input := map[string]any{"username": "new-student", "display_name": "新同学", "password": "disposable-student-password"}
	request("/api/v1/auth/register", input, "https://evil.test", 403)
	request("/api/v1/auth/register", input, "https://www.sumobai.com", 201)
	var role string
	var group int
	var mustChange bool
	if err = db.QueryRow(`SELECT role,group_id,must_change_password FROM users WHERE username='new-student'`).Scan(&role, &group, &mustChange); err != nil || role != "learner" || group != 1 || mustChange {
		t.Fatal(role, group, mustChange, err)
	}
	request("/api/v1/auth/register", input, "https://www.sumobai.com", 409)
	input["role"] = "admin"
	request("/api/v1/auth/register", input, "https://www.sumobai.com", 400)
	delete(input, "role")
	login := request("/api/v1/auth/login", map[string]string{"username": "new-student", "password": "disposable-student-password"}, "https://www.sumobai.com", 200)
	cookie := login.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly {
		t.Fatal(cookie)
	}
	r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var me struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &me)
	// Password changes require CSRF and revoke all existing sessions.
	raw, _ := json.Marshal(map[string]string{"current_password": "disposable-student-password", "new_password": "new-disposable-password"})
	r = httptest.NewRequest("POST", "/api/v1/auth/password", bytes.NewReader(raw))
	r.Header.Set("Origin", "https://www.sumobai.com")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("missing CSRF was allowed")
	}
	r = httptest.NewRequest("POST", "/api/v1/auth/password", bytes.NewReader(raw))
	r.Header.Set("Origin", "https://www.sumobai.com")
	r.Header.Set("X-CSRF-Token", me.CSRF)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	db.Exec(`UPDATE account_policy SET registration_open=false`)
	input["username"] = "another-student"
	request("/api/v1/auth/register", input, "https://www.sumobai.com", 403)
}
func TestServerPDFDirectoryConfinement(t *testing.T) {
	dir := t.TempDir()
	data, _ := os.ReadFile("testdata/questions.pdf")
	if err := os.WriteFile(filepath.Join(dir, "questions.pdf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalPDF(dir, "questions.pdf"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.pdf")
	os.WriteFile(outside, data, 0600)
	os.Symlink(outside, filepath.Join(dir, "escape.pdf"))
	for _, name := range []string{"../private.pdf", "/etc/passwd", "escape.pdf", "questions.pdf/../private.pdf", ".env", "a\\b.pdf"} {
		if _, err := readLocalPDF(dir, name); err == nil {
			t.Fatal("unsafe path accepted", name)
		}
	}
}
func TestAdditiveUpgradePreservesAccountsAndEncryptedKeys(t *testing.T) {
	s, _, _ := learningTestServer(t)
	ctx := context.Background()
	cipher, err := security.Seal(s.encryptionKey, "disposable-key", "provider:upgrade")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key) VALUES('upgrade','Upgrade','https://provider.test/v1',$1)`, cipher)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&before)
	if err = migrate(ctx, s.db); err != nil {
		t.Fatal(err)
	}
	if err = s.initLearning(ctx); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	var after int
	s.db.QueryRow(`SELECT api_key FROM ai_providers WHERE id='upgrade'`).Scan(&stored)
	s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&after)
	if !bytes.Equal(cipher, stored) || before != after {
		t.Fatal("upgrade changed accounts or encrypted keys")
	}
}
