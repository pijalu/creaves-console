//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUsersUpdateTestApp mounts PUT /users/:user_id against the shared testDB
// with a fake signed-in caller (admin flag configurable).
func newUsersUpdateTestApp(tx *pop.Connection, caller *models.User) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			if caller != nil {
				c.Set("current_user", caller)
			}
			return next(c)
		}
	})
	app.PUT("/users/{user_id}", UsersResource{}.Update)
	return app
}

func createConsoleTestUser(t *testing.T, login string, admin, active bool) *models.User {
	t.Helper()
	u := &models.User{
		ID:    uuid.Must(uuid.NewV4()),
		Login: login,
		Name:  login,
		Admin: admin, Active: active,
	}
	require.NoError(t, u.SetPasswordHash())
	u.Password = "testpassword123"
	u.PasswordConfirmation = "testpassword123"
	_, err := testDB.ValidateAndCreate(u)
	require.NoError(t, err)
	t.Cleanup(func() { testDB.Destroy(u) })
	return u
}

// TestUserUpdateNonAdminCannotPromoteSelf is the regression test for the
// privilege-escalation bug: a non-admin caller PUTting admin=true on their own
// record must not change admin/active flags.
func TestUserUpdateNonAdminCannotPromoteSelf(t *testing.T) {
	victim := createConsoleTestUser(t, "plainviewer", false, true)
	caller := &models.User{ID: victim.ID, Login: victim.Login, Name: victim.Name, Admin: false, Active: true}

	app := newUsersUpdateTestApp(testDB, caller)

	form := url.Values{
		"login":    {"plainviewer"},
		"name":     {"plainviewer"},
		"email":    {"v@example.com"},
		"admin":    {"true"},
		"active":   {"true"},
		"password": {""},
	}
	req := httptest.NewRequest(http.MethodPut, "/users/"+victim.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	require.Equal(t, http.StatusSeeOther, res.Code, "body: %s", res.Body.String())

	reloaded := &models.User{}
	require.NoError(t, testDB.Find(reloaded, victim.ID))
	assert.False(t, reloaded.Admin, "non-admin caller must not be able to set admin=true")
	assert.True(t, reloaded.Active, "active flag must be preserved")
}

// TestUserUpdateAdminCannotRemoveOwnAdmin guards the self-lockout protection:
// an admin editing their own account keeps the admin flag.
func TestUserUpdateAdminCannotRemoveOwnAdmin(t *testing.T) {
	admin := createConsoleTestUser(t, "selfadmin", true, true)
	caller := &models.User{ID: admin.ID, Login: admin.Login, Name: admin.Name, Admin: true, Active: true}

	app := newUsersUpdateTestApp(testDB, caller)

	// admin flag intentionally absent from form (checkbox unchecked)
	form := url.Values{
		"login": {"selfadmin"},
		"name":  {"selfadmin"},
		"email": {"a@example.com"},
	}
	req := httptest.NewRequest(http.MethodPut, "/users/"+admin.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	require.Equal(t, http.StatusSeeOther, res.Code, "body: %s", res.Body.String())

	reloaded := &models.User{}
	require.NoError(t, testDB.Find(reloaded, admin.ID))
	assert.True(t, reloaded.Admin, "admin editing self must keep admin flag")
}

// TestUserJSONNeverExposesPasswordHash: the JSON rendering of a user must not
// contain the password hash.
func TestUserJSONNeverExposesPasswordHash(t *testing.T) {
	u := &models.User{Login: "jsontest", Name: "JSON", PasswordHash: "$2a$10$secrethash", Admin: true, Active: true}
	out, err := json.Marshal(u)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "password_hash")
	assert.NotContains(t, string(out), "secrethash")
}

// TestWebhookAPIKeyJSONNeverExposesKeyHash: API key JSON must not leak the hash.
func TestWebhookAPIKeyJSONNeverExposesKeyHash(t *testing.T) {
	k := &models.WebhookAPIKey{Name: "k", KeyHash: "$2a$10$keyhashsecret", KeyPrefix: "abcd1234"}
	out, err := json.Marshal(k)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "key_hash")
	assert.NotContains(t, string(out), "keyhashsecret")
}

// TestWebhookRejectsOversizedBody: bodies above the cap must get 413.
func TestWebhookRejectsOversizedBody(t *testing.T) {
	// Create a valid active API key
	rawKey, hash, prefix, err := models.GenerateKey()
	require.NoError(t, err)
	key := &models.WebhookAPIKey{
		ID: uuid.Must(uuid.NewV4()), Name: "oversize-test",
		KeyHash: hash, KeyPrefix: prefix, Active: true,
	}
	require.NoError(t, testDB.Create(key))
	t.Cleanup(func() { testDB.Destroy(key) })

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", testDB)
			return next(c)
		}
	})
	app.POST("/webhook/events", WebhookEventsHandler)

	big := strings.NewReader(`{"events":[{"id":"` + strings.Repeat("a", 11<<20) + `"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/events", big)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, res.Code, "body: %.200s", res.Body.String())
}

// TestWebhookRejectsTooManyEvents: batches above the cap must get 413.
func TestWebhookRejectsTooManyEvents(t *testing.T) {
	rawKey, hash, prefix, err := models.GenerateKey()
	require.NoError(t, err)
	key := &models.WebhookAPIKey{
		ID: uuid.Must(uuid.NewV4()), Name: "toomany-test",
		KeyHash: hash, KeyPrefix: prefix, Active: true,
	}
	require.NoError(t, testDB.Create(key))
	t.Cleanup(func() { testDB.Destroy(key) })

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", testDB)
			return next(c)
		}
	})
	app.POST("/webhook/events", WebhookEventsHandler)

	var b strings.Builder
	b.WriteString(`{"events":[`)
	for i := 0; i < 1001; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"550e8400-e29b-41d4-a716-446655440000","instance_id":"x","animal_id":1,"event_type":"animal_died","payload":{},"created_at":"2024-01-15T10:30:00Z"}`)
	}
	b.WriteString(`]}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook/events", strings.NewReader(b.String()))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, res.Code, "body: %.200s", res.Body.String())
}
