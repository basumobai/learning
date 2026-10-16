package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"example.com/els/api/internal/security"
)

type identity struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	GroupID     int64  `json:"group_id"`
	MustChange  bool   `json:"must_change_password"`
	MFA         bool   `json:"mfa_enabled"`
	sessionHash string
	csrf        string
}
type identityKey struct{}

func account(ctx context.Context) identity {
	user, _ := ctx.Value(identityKey{}).(identity)
	return user
}
func userID(ctx context.Context) string { return account(ctx).ID }

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,39}$`)
var dummyPassword = security.HashPassword(security.Token(32))
var loginHashSlots = make(chan struct{}, 4)

func configure(db *sql.DB) (*server, error) {
	origin := os.Getenv("APP_ORIGIN")
	if origin == "" {
		origin = "http://localhost:3000"
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("APP_ORIGIN must be one web origin")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("public APP_ORIGIN requires HTTPS")
	}
	key := []byte(nil)
	if raw := os.Getenv("APP_ENCRYPTION_KEY"); raw != "" {
		key, err = base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("APP_ENCRYPTION_KEY must be a base64 32-byte key")
		}
	}
	return &server{db: db, origin: u.Scheme + "://" + u.Host, secureCookie: u.Scheme == "https", encryptionKey: key, client: newProviderClient()}, nil
}
func (s *server) bootstrap(ctx context.Context) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role='admin' AND password_hash IS NOT NULL AND NOT disabled)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	username := strings.ToLower(strings.TrimSpace(os.Getenv("ELS_ADMIN_USERNAME")))
	password := os.Getenv("ELS_ADMIN_PASSWORD")
	if !usernamePattern.MatchString(username) || !security.PasswordValid(password) {
		return fmt.Errorf("set ELS_ADMIN_USERNAME and a 15–128 character ELS_ADMIN_PASSWORD for initial administrator")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(17283940)`); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role='admin' AND password_hash IS NOT NULL AND NOT disabled)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit()
	}
	id := security.Token(18)
	_, err = tx.ExecContext(ctx, `INSERT INTO users(id,username,display_name,password_hash,role,must_change_password) VALUES($1,$2,'管理员',$3,'admin',true)`, id, username, security.HashPassword(password))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_audit(actor_id,action,target) VALUES($1,'bootstrap_admin',$1)`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *server) sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == s.origin
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site" && r.Header.Get("X-ELS-Client") == "web"
}
func (s *server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		unsafe := r.Method != "GET" && r.Method != "HEAD"
		if unsafe && !s.sameOrigin(r) {
			fail(w, 403, "请求来源与站点配置不一致，请从正确域名访问；若仍失败，请联系管理员核对访问域名配置")
			return
		}
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/options" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(s.cookieName())
		if err != nil || len(cookie.Value) > 128 {
			fail(w, 401, "请先登录")
			return
		}
		user := identity{sessionHash: security.Digest(cookie.Value), csrf: security.CSRF(cookie.Value)}
		err = s.db.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.display_name,u.role,u.group_id,u.must_change_password,u.totp_secret IS NOT NULL
   FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.token_hash=$1 AND a.expires_at>now() AND NOT u.disabled
   AND a.last_seen_at>now()-CASE WHEN u.role='admin' THEN interval '1 hour' ELSE interval '24 hours' END`, user.sessionHash).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.GroupID, &user.MustChange, &user.MFA)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "登录已过期，请重新登录")
			return
		}
		if err != nil {
			s.dbError(w, "authenticate", err)
			return
		}
		if unsafe && !security.Equal(user.csrf, r.Header.Get("X-CSRF-Token")) {
			fail(w, 403, "安全校验失效，请刷新页面")
			return
		}
		if user.MustChange && r.URL.Path != "/api/v1/auth/me" && r.URL.Path != "/api/v1/auth/password" && r.URL.Path != "/api/v1/auth/logout" {
			fail(w, 403, "请先更改初始密码")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") && user.Role != "admin" {
			fail(w, 403, "需要管理员权限")
			return
		}
		if _, err = s.db.ExecContext(r.Context(), `UPDATE auth_sessions SET last_seen_at=now() WHERE token_hash=$1 AND last_seen_at<now()-interval '5 minutes'`, user.sessionHash); err != nil {
			s.dbError(w, "session activity", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
	})
}
func (s *server) cookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 24 * 3600})
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	select {
	case loginHashSlots <- struct{}{}:
		defer func() { <-loginHashSlots }()
	default:
		fail(w, 503, "登录服务繁忙，请稍后重试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if decode(r, &input) != nil || len(input.Password) > 1024 {
		fail(w, 400, "登录信息无效")
		return
	}
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	keys := []string{security.Digest("user:" + input.Username), security.Digest("ip:" + ip)}
	for _, key := range keys {
		var blocked bool
		err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(blocked_until>now(),false) FROM login_attempts WHERE key=$1`, key).Scan(&blocked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.dbError(w, "login guard", err)
			return
		}
		if blocked {
			fail(w, 429, "登录尝试过多，请稍后再试")
			return
		}
	}
	var id, hash string
	var secret []byte
	var lastStep int64
	var disabled bool
	err := s.db.QueryRowContext(r.Context(), `SELECT id,password_hash,disabled,totp_secret,totp_last_step FROM users WHERE username=$1 AND password_hash IS NOT NULL`, input.Username).Scan(&id, &hash, &disabled, &secret, &lastStep)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.dbError(w, "login user", err)
		return
	}
	if hash == "" {
		hash = dummyPassword
	}
	valid := security.VerifyPassword(input.Password, hash) && id != "" && !disabled
	step := int64(-1)
	if valid && len(secret) > 0 {
		plain, err := security.Open(s.encryptionKey, secret, "totp:"+id)
		if err != nil {
			fail(w, 503, "账户验证配置暂不可用，请联系管理员")
			return
		}
		step, valid = security.TOTPStep(plain, input.Code, time.Now(), lastStep)
	}
	if !valid {
		for _, key := range keys {
			threshold := 8
			if key == keys[1] {
				threshold = 40
			}
			_, e := s.db.ExecContext(r.Context(), `INSERT INTO login_attempts(key,failures) VALUES($1,1) ON CONFLICT(key) DO UPDATE SET
 failures=CASE WHEN login_attempts.started_at<now()-interval '15 minutes' THEN 1 ELSE login_attempts.failures+1 END,
 started_at=CASE WHEN login_attempts.started_at<now()-interval '15 minutes' THEN now() ELSE login_attempts.started_at END,
 blocked_until=CASE WHEN login_attempts.started_at>=now()-interval '15 minutes' AND login_attempts.failures+1>=$2 THEN now()+interval '15 minutes' ELSE NULL END`, key, threshold)
			if e != nil {
				s.dbError(w, "login failure", e)
				return
			}
		}
		fail(w, 401, "用户名、密码或验证码不正确")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "login", err)
		return
	}
	defer tx.Rollback()
	// Recheck credentials while holding the row: a concurrent reset/disable must revoke this login too.
	var currentHash string
	var currentSecret []byte
	var currentStep int64
	err = tx.QueryRowContext(r.Context(), `SELECT password_hash,totp_secret,totp_last_step FROM users WHERE id=$1 AND NOT disabled FOR UPDATE`, id).Scan(&currentHash, &currentSecret, &currentStep)
	if err != nil || currentHash != hash || !security.Equal(string(secret), string(currentSecret)) || (step >= 0 && step <= currentStep) {
		fail(w, 401, "用户名、密码或验证码不正确")
		return
	}
	if step >= 0 {
		if _, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_last_step=$2 WHERE id=$1`, id, step); err != nil {
			s.dbError(w, "mfa replay", err)
			return
		}
	}
	token := security.Token(32)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO auth_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')`, security.Digest(token), id); err != nil {
		s.dbError(w, "login session", err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM login_attempts WHERE key=$1`, keys[0]); err != nil {
		s.dbError(w, "login guard reset", err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE expires_at<now()`); err != nil {
		s.dbError(w, "session cleanup", err)
		return
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "login commit", err)
		return
	}
	s.cookie(w, token)
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	user := account(r.Context())
	respond(w, 200, map[string]any{"user": user, "csrf_token": user.csrf})
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE token_hash=$1`, account(r.Context()).sessionHash); err != nil {
		s.dbError(w, "logout", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *server) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	if decode(r, &input) != nil || !security.PasswordValid(input.Password) || input.Current == input.Password {
		fail(w, 400, "新密码应为 15–128 个字符，并与旧密码不同")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "password", err)
		return
	}
	defer tx.Rollback()
	var old string
	if err = tx.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1`, userID(r.Context())).Scan(&old); err != nil {
		s.dbError(w, "password read", err)
		return
	}
	if !security.VerifyPassword(input.Current, old) {
		fail(w, 400, "当前密码不正确")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=$2,must_change_password=false WHERE id=$1`, userID(r.Context()), security.HashPassword(input.Password)); err != nil {
		s.dbError(w, "password save", err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1`, userID(r.Context())); err != nil {
		s.dbError(w, "revoke sessions", err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO account_audit(actor_id,action,target) VALUES($1,'change_password',$1)`, userID(r.Context())); err != nil {
		s.dbError(w, "password audit", err)
		return
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "password commit", err)
		return
	}
	respond(w, 200, map[string]bool{"login_required": true})
}
func (s *server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1 AND token_hash<>$2`, userID(r.Context()), account(r.Context()).sessionHash)
	if err != nil {
		s.dbError(w, "revoke sessions", err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *server) mfa(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action   string `json:"action"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if decode(r, &input) != nil {
		fail(w, 400, "验证信息无效")
		return
	}
	tx, err := s.studyTx(r.Context())
	if err != nil {
		s.dbError(w, "mfa", err)
		return
	}
	defer tx.Rollback()
	id := userID(r.Context())
	var hash string
	var secret, pending []byte
	var expiry sql.NullTime
	var last int64
	err = tx.QueryRowContext(r.Context(), `SELECT password_hash,totp_secret,totp_pending,totp_pending_expires,totp_last_step FROM users WHERE id=$1`, id).Scan(&hash, &secret, &pending, &expiry, &last)
	if err != nil {
		s.dbError(w, "mfa read", err)
		return
	}
	if !security.VerifyPassword(input.Password, hash) {
		fail(w, 400, "当前密码不正确")
		return
	}
	var result any = map[string]bool{"ok": true}
	switch input.Action {
	case "start":
		if len(secret) > 0 {
			fail(w, 409, "两步验证已开启")
			return
		}
		plain := security.NewTOTPSecret()
		encrypted, err := security.Seal(s.encryptionKey, plain, "totp:"+id)
		if err != nil {
			fail(w, 503, "请先配置服务器加密密钥")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_pending=$2,totp_pending_expires=now()+interval '10 minutes' WHERE id=$1`, id, encrypted)
		if err != nil {
			s.dbError(w, "mfa enrollment", err)
			return
		}
		result = map[string]string{"secret": plain, "uri": "otpauth://totp/ELS:" + url.PathEscape(account(r.Context()).Username) + "?secret=" + plain + "&issuer=ELS&algorithm=SHA1&digits=6&period=30"}
	case "confirm":
		if len(pending) == 0 || !expiry.Valid || expiry.Time.Before(time.Now()) || len(secret) > 0 {
			fail(w, 400, "请重新开始绑定")
			return
		}
		plain, err := security.Open(s.encryptionKey, pending, "totp:"+id)
		if err != nil {
			fail(w, 503, "账户验证配置暂不可用")
			return
		}
		step, ok := security.TOTPStep(plain, input.Code, time.Now(), -1)
		if !ok {
			fail(w, 400, "验证码不正确")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_secret=totp_pending,totp_pending=NULL,totp_pending_expires=NULL,totp_last_step=$2 WHERE id=$1`, id, step)
		if err != nil {
			s.dbError(w, "mfa confirm", err)
			return
		}
	case "disable":
		if len(secret) == 0 {
			fail(w, 400, "尚未开启两步验证")
			return
		}
		plain, err := security.Open(s.encryptionKey, secret, "totp:"+id)
		if err != nil {
			fail(w, 503, "账户验证配置暂不可用")
			return
		}
		_, ok := security.TOTPStep(plain, input.Code, time.Now(), last)
		if !ok {
			fail(w, 400, "验证码无效或已使用")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_secret=NULL,totp_pending=NULL,totp_pending_expires=NULL,totp_last_step=-1 WHERE id=$1`, id)
		if err != nil {
			s.dbError(w, "mfa disable", err)
			return
		}
	default:
		fail(w, 400, "操作无效")
		return
	}
	if input.Action != "start" {
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id=$1 AND token_hash<>$2`, id, account(r.Context()).sessionHash); err != nil {
			s.dbError(w, "mfa revoke", err)
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO account_audit(actor_id,action,target) VALUES($1,$2,$1)`, id, "mfa_"+input.Action); err != nil {
			s.dbError(w, "mfa audit", err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		s.dbError(w, "mfa commit", err)
		return
	}
	respond(w, 200, result)
}

func (s *server) cookieName() string {
	if s.secureCookie {
		return "__Host-els_session"
	}
	return "els_session"
}
