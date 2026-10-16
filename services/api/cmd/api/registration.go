package main

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"

	"example.com/els/api/internal/security"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *server) registrationOptions(w http.ResponseWriter, r *http.Request) {
	var open bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT registration_open FROM account_policy WHERE singleton`).Scan(&open); err != nil {
		s.dbError(w, "registration options", err)
		return
	}
	respond(w, 200, map[string]bool{"registration_open": open})
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "注册信息无效")
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !usernamePattern.MatchString(in.Username) || !security.PasswordValid(in.Password) || in.DisplayName == "" || len([]rune(in.DisplayName)) > 80 {
		fail(w, 400, "用户名需3–40位小写字母、数字或_.-，密码需15–128个字符，显示名称需1–80字")
		return
	}
	select {
	case loginHashSlots <- struct{}{}:
		defer func() { <-loginHashSlots }()
	default:
		fail(w, 503, "注册服务繁忙，请稍后重试")
		return
	}
	var open bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT registration_open FROM account_policy WHERE singleton`).Scan(&open); err != nil {
		s.dbError(w, "registration policy", err)
		return
	}
	if !open {
		fail(w, 403, "本站暂未开放自主注册，请联系管理员")
		return
	}
	// Do not trust forwarded IPs from browsers. This limit also bounds work behind a proxy.
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	var attempts int
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO registration_attempts(key,attempts) VALUES($1,1) ON CONFLICT(key) DO UPDATE SET attempts=CASE WHEN registration_attempts.started_at<now()-interval '1 minute' THEN 1 ELSE registration_attempts.attempts+1 END,started_at=CASE WHEN registration_attempts.started_at<now()-interval '1 minute' THEN now() ELSE registration_attempts.started_at END RETURNING attempts`, security.Digest("register:"+peer)).Scan(&attempts)
	if err != nil {
		s.dbError(w, "registration guard", err)
		return
	}
	if attempts > 20 {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "注册尝试过多，请稍后再试")
		return
	}
	hash := security.HashPassword(in.Password)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "registration", err)
		return
	}
	defer tx.Rollback()
	var group int64
	// Lock policy so closing registration or changing its group is atomic with signup.
	err = tx.QueryRowContext(r.Context(), `SELECT default_group_id FROM account_policy WHERE singleton AND registration_open FOR SHARE`).Scan(&group)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 403, "本站暂未开放自主注册，请联系管理员")
		return
	}
	if err != nil {
		s.dbError(w, "registration policy", err)
		return
	}
	id := security.Token(18)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO users(id,username,display_name,password_hash,role,group_id,must_change_password) VALUES($1,$2,$3,$4,'learner',$5,false)`, id, in.Username, in.DisplayName, hash, group)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		fail(w, 409, "此用户名不可用，请换一个")
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO account_audit(actor_id,action,target) VALUES($1,'self_register',$1)`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.dbError(w, "create learner", err)
		return
	}
	respond(w, 201, map[string]bool{"ok": true})
}

func (s *server) registrationPolicy(w http.ResponseWriter, r *http.Request) {
	var open bool
	var group int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT registration_open,default_group_id FROM account_policy WHERE singleton`).Scan(&open, &group); err != nil {
		s.dbError(w, "account policy", err)
		return
	}
	respond(w, 200, map[string]any{"registration_open": open, "default_group_id": group})
}
func (s *server) saveRegistrationPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Open     bool   `json:"registration_open"`
		Group    int64  `json:"default_group_id"`
		Password string `json:"admin_password"`
	}
	if decode(r, &in) != nil || in.Group < 1 {
		fail(w, 400, "注册设置无效")
		return
	}
	if !s.adminPassword(w, r, in.Password) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.dbError(w, "account policy", err)
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE account_policy SET registration_open=$1,default_group_id=$2 WHERE singleton`, in.Open, in.Group)
	if err == nil {
		err = audit(tx, r, "registration_policy", "site")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 400, "保存失败，请确认默认身份组存在")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
