package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebhookAPIKeyString(t *testing.T) {
	w := WebhookAPIKey{
		Name:       "ci-key",
		KeyPrefix:  "abc12345",
		InstanceID: "prod-instance",
		Active:     true,
	}

	out := w.String()

	// String() must return valid JSON that round-trips back into the struct.
	var got WebhookAPIKey
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if got.Name != "ci-key" {
		t.Errorf("expected Name %q, got %q", "ci-key", got.Name)
	}
	if got.KeyPrefix != "abc12345" {
		t.Errorf("expected KeyPrefix %q, got %q", "abc12345", got.KeyPrefix)
	}
	if !got.Active {
		t.Errorf("expected Active true, got false")
	}
}

func TestWebhookAPIKeysString(t *testing.T) {
	ws := WebhookAPIKeys{
		{Name: "first", InstanceID: "a"},
		{Name: "second", InstanceID: "b"},
	}

	out := ws.String()

	var got WebhookAPIKeys
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if got[1].Name != "second" {
		t.Errorf("expected second entry Name %q, got %q", "second", got[1].Name)
	}
}

func TestGenerateKey(t *testing.T) {
	rawKey, hash, prefix, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	if !strings.HasPrefix(rawKey, "creaves_") {
		t.Errorf("expected raw key to start with %q, got %q", "creaves_", rawKey)
	}

	if len(prefix) != 8 {
		t.Errorf("expected prefix length 8, got %d (%q)", len(prefix), prefix)
	}

	// The hash must be a bcrypt hash and must validate the generated raw key.
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected bcrypt hash starting with $2, got %q", hash)
	}

	// The prefix must be the first 8 chars of the uuid embedded in the raw key.
	embeddedUUID := strings.TrimPrefix(rawKey, "creaves_")
	if want := embeddedUUID[:8]; prefix != want {
		t.Errorf("expected prefix %q, got %q", want, prefix)
	}

	// The key authenticates against its own hash.
	w := &WebhookAPIKey{KeyHash: hash}
	if !w.Authenticate(rawKey) {
		t.Error("generated raw key did not authenticate against its own hash")
	}
}

func TestGenerateKeyUniqueness(t *testing.T) {
	k1, _, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("first GenerateKey error: %v", err)
	}
	k2, _, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("second GenerateKey error: %v", err)
	}
	if k1 == k2 {
		t.Error("expected two generated keys to differ, they were identical")
	}
}

func TestWebhookAPIKeyAuthenticate(t *testing.T) {
	rawKey, hash, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	w := &WebhookAPIKey{KeyHash: hash}

	if !w.Authenticate(rawKey) {
		t.Error("expected Authenticate to return true for the matching key")
	}
	if w.Authenticate("creaves_wrong-key") {
		t.Error("expected Authenticate to return false for a wrong key")
	}
	if w.Authenticate("") {
		t.Error("expected Authenticate to return false for an empty key")
	}
}
