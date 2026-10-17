package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"example.com/els/api/internal/security"
	"golang.org/x/sys/unix"
)

func (s *server) adminSession(w http.ResponseWriter, r *http.Request) bool {
	var permitted bool
	err := s.db.QueryRowContext(r.Context(), `SELECT role='admin' AND NOT disabled AND NOT must_change_password FROM users WHERE id=$1`, userID(r.Context())).Scan(&permitted)
	if err != nil || !permitted {
		fail(w, 403, "需要有效的管理员会话")
		return false
	}
	return true
}

func resetAdministrator(ctx context.Context, db *sql.DB, username, password string, clearMFA bool) error {
	if !usernamePattern.MatchString(username) || !security.PasswordValid(password) {
		return fmt.Errorf("用户名或密码无效（密码需15–128字符）")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	var disabled bool
	if err = tx.QueryRowContext(ctx, `SELECT id,disabled FROM users WHERE username=$1 AND role='admin' FOR UPDATE`, username).Scan(&id, &disabled); err != nil {
		return fmt.Errorf("未找到管理员账号")
	}
	if disabled {
		return fmt.Errorf("管理员已被停用，密码恢复不会重新启用账号")
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=$2,must_change_password=true,
 totp_secret=CASE WHEN $3 THEN NULL ELSE totp_secret END,totp_pending=NULL,totp_pending_expires=NULL,
 totp_last_step=CASE WHEN $3 THEN -1 ELSE totp_last_step END WHERE id=$1`, id, security.HashPassword(password), clearMFA)
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=$1`, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO account_audit(action,target) VALUES('server_admin_password_reset',$1)`, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}

func adminRecoveryCommand(ctx context.Context, db *sql.DB, args []string) error {
	f := flag.NewFlagSet("admin-reset-password", flag.ContinueOnError)
	username := f.String("username", "", "administrator username")
	clearMFA := f.Bool("clear-mfa", false, "also reset lost authenticator")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !usernamePattern.MatchString(*username) || f.NArg() != 0 {
		return fmt.Errorf("usage: /app/els admin-reset-password --username admin [--clear-mfa]")
	}
	term, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	if err != nil {
		return fmt.Errorf("恢复命令需要交互终端，请使用 docker compose exec -it api /app/els admin-reset-password --username admin")
	}
	reader := bufio.NewReader(os.Stdin)
	hidden := func(label string) (string, error) {
		fmt.Fprint(os.Stderr, label)
		changed := *term
		changed.Lflag &^= unix.ECHO
		if err := unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, &changed); err != nil {
			return "", err
		}
		defer unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, term)
		text, err := reader.ReadString('\n')
		fmt.Fprintln(os.Stderr)
		return strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r"), err
	}
	password, err := hidden("新的临时密码（不回显）：")
	if err != nil {
		return err
	}
	again, err := hidden("再次输入：")
	if err != nil {
		return err
	}
	if password != again {
		return fmt.Errorf("两次密码不同，未修改账号")
	}
	fmt.Fprintf(os.Stderr, "将撤销 %s 的所有会话并要求首次改密；清除验证器=%v。输入 RESET %s 确认：", *username, *clearMFA, *username)
	confirm, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(confirm) != "RESET "+*username {
		return fmt.Errorf("已取消，未修改账号")
	}
	if err = resetAdministrator(ctx, db, *username, password, *clearMFA); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "密码已重置。请登录后立即更改临时密码；数据库和加密密钥保持原值。")
	return nil
}
