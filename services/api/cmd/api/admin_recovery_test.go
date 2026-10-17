package main

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestOrdinaryManagementUsesSessionAndKeepsStepUp(t *testing.T) {
	s, student, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	provider := map[string]any{"name": "No password needed", "base_url": "https://provider.test/v1", "api_key": "temporary-key", "enabled": true}
	admin.call("POST", "/api/v1/admin/providers", provider, 200)
	student.call("POST", "/api/v1/admin/providers", provider, 403)
	r := admin.request("POST", "/api/v1/admin/providers", provider)
	r.Header.Del("X-CSRF-Token")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF removed", w.Code)
	}
	admin.call("POST", "/api/v1/admin/users", map[string]any{"username": "new-user", "display_name": "User", "role": "learner", "group_id": 1, "password": "new-testing-password"}, 403)
}
func TestServerRecoveryRevokesSessionsAndRequiresChange(t *testing.T) {
	s, _, _ := learningTestServer(t)
	admin := curriculumAdmin(t, s)
	if err := resetAdministrator(context.Background(), s.db, "teacher", "recovery-temporary-password", false); err != nil {
		t.Fatal(err)
	}
	admin.call("GET", "/api/v1/auth/me", nil, 401)
	fresh := &apiTester{t: t, handler: s.routes()}
	fresh.login("teacher", "recovery-temporary-password", "")
	fresh.call("GET", "/api/v1/admin/config", nil, 403)
	fresh.call("POST", "/api/v1/auth/password", map[string]string{"current_password": "recovery-temporary-password", "new_password": "recovery-permanent-password"}, 200)
	fresh.login("teacher", "recovery-permanent-password", "")
	fresh.call("GET", "/api/v1/admin/config", nil, 200)
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM account_audit WHERE action='server_admin_password_reset'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("recovery unaudited", count, err)
	}
	s.db.Exec(`UPDATE users SET disabled=true WHERE username='teacher'`)
	if resetAdministrator(context.Background(), s.db, "teacher", "another-testing-password", true) == nil {
		t.Fatal("recovery enabled a disabled administrator")
	}
}
func TestBootstrapSetsInitialPasswordGate(t *testing.T) {
	s := &server{db: testDatabase(t)}
	t.Setenv("ELS_ADMIN_USERNAME", "first-admin")
	t.Setenv("ELS_ADMIN_PASSWORD", "first-temporary-password")
	if err := s.bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	var required bool
	if err := s.db.QueryRow(`SELECT must_change_password FROM users WHERE username='first-admin'`).Scan(&required); err != nil || !required {
		t.Fatal("initial password gate missing", err)
	}
}
