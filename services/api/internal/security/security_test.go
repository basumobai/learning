package security

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordsEncryptionAndCSRF(t *testing.T) {
	password := "long Chinese 密码 phrase 123"
	a, b := HashPassword(password), HashPassword(password)
	if a == b || !VerifyPassword(password, a) || VerifyPassword("incorrect", a) || VerifyPassword(password, "$argon2id$bad") {
		t.Fatal("password salt/verification failed")
	}
	if PasswordValid("12345678") || !PasswordValid(password) || PasswordValid(strings.Repeat("字", 129)) {
		t.Fatal("password length rule failed")
	}
	key := make([]byte, 32)
	encrypted, err := Seal(key, "api-secret", "provider:1")
	if err != nil {
		t.Fatal(err)
	}
	text, err := Open(key, encrypted, "provider:1")
	if err != nil || text != "api-secret" {
		t.Fatal("encryption failed")
	}
	if _, err = Open(key, encrypted, "provider:2"); err == nil {
		t.Fatal("AAD swapping accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = Open(key, encrypted, "provider:1"); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err = Seal(nil, "secret", "a"); err == nil {
		t.Fatal("empty encryption key accepted")
	}
	token := Token(32)
	if len(token) < 40 || CSRF(token) == CSRF(Token(32)) || Equal(CSRF(token), "forged") {
		t.Fatal("session/CSRF entropy failed")
	}
}
func TestTOTPAgainstRFCVectorAndReplay(t *testing.T) {
	// RFC 6238 SHA-1 secret with the 6-digit truncation used by authenticators.
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(59, 0)
	if TOTP(secret, 1) != "287082" {
		t.Fatal("RFC TOTP vector failed")
	}
	step, ok := TOTPStep(secret, "287082", now, -1)
	if !ok || step != 1 {
		t.Fatal("valid TOTP rejected")
	}
	if _, ok = TOTPStep(secret, "287082", now, step); ok {
		t.Fatal("replayed TOTP accepted")
	}
	if _, ok = TOTPStep(secret, "not-a-code", now, -1); ok {
		t.Fatal("invalid TOTP accepted")
	}
}
