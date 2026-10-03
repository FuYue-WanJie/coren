package webauth

import (
	"testing"
	"time"
)

func TestLoginRejectsWrongPassword(t *testing.T) {
	s := New("secret", time.Hour)
	if _, _, ok := s.Login("nope"); ok {
		t.Fatal("wrong password accepted")
	}
	if _, _, ok := s.Login("secret"); !ok {
		t.Fatal("correct password rejected")
	}
}

func TestTokenValidUntilExpiry(t *testing.T) {
	s := New("pw", time.Hour)
	now := time.Now()
	s.now = func() time.Time { return now }
	token, _, ok := s.Login("pw")
	if !ok {
		t.Fatal("login failed")
	}
	if !s.Validate(token) {
		t.Fatal("token should be valid")
	}
	now = now.Add(2 * time.Hour)
	if s.Validate(token) {
		t.Fatal("token should have expired")
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	s := New("pw", time.Hour)
	token, _, _ := s.Login("pw")
	s.Logout(token)
	if s.Validate(token) {
		t.Fatal("logged-out token still valid")
	}
}

func TestEmptyPasswordDisablesAuth(t *testing.T) {
	s := New("", time.Hour)
	if s.Required() {
		t.Fatal("empty password should not require auth")
	}
	if !s.Validate("anything") {
		t.Fatal("auth-disabled store should accept any token")
	}
}

func TestValidateRejectsUnknownToken(t *testing.T) {
	s := New("pw", time.Hour)
	if s.Validate("forged-token") {
		t.Fatal("unknown token accepted")
	}
}
