package models

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestUserSetPasswordHash(t *testing.T) {
	u := &User{Password: "supersecret"}

	if err := u.SetPasswordHash(); err != nil {
		t.Fatalf("SetPasswordHash returned error: %v", err)
	}

	if u.PasswordHash == "" {
		t.Fatal("expected PasswordHash to be populated")
	}
	// The hash must never equal the plaintext password.
	if u.PasswordHash == "supersecret" {
		t.Error("PasswordHash must not store plaintext password")
	}
	// The hash must verify against the original password via bcrypt.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("supersecret")); err != nil {
		t.Errorf("hashed password did not verify via bcrypt: %v", err)
	}
	// And must NOT verify against a different password.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("wrongpass")); err == nil {
		t.Error("expected bcrypt verification to fail for a wrong password")
	}
}

func TestUserString(t *testing.T) {
	u := User{Login: "alice", Name: "Alice", Email: "alice@example.com", Admin: true}

	out := u.String()

	var got User
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if got.Login != "alice" {
		t.Errorf("expected Login %q, got %q", "alice", got.Login)
	}
	if !got.Admin {
		t.Error("expected Admin true")
	}
	// Password fields use json:"-" and must not leak into the JSON output.
	if strings.Contains(out, "supersecret") {
		t.Error("password value leaked into String() JSON output")
	}
}

func TestUsersString(t *testing.T) {
	us := Users{
		{Login: "alice"},
		{Login: "bob"},
	}

	out := us.String()

	var got Users
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 users, got %d", len(got))
	}
	if got[0].Login != "alice" || got[1].Login != "bob" {
		t.Errorf("unexpected logins: %q, %q", got[0].Login, got[1].Login)
	}
}
