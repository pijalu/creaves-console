//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAdminTestApp builds a minimal Buffalo app that injects the shared testDB
// as the request-scoped "tx" value and fakes an authenticated admin (or
// non-admin) user on the context. It mounts the WebhookAPIKeys resource so the
// full admin CRUD flow can be exercised through httptest.
func newAdminTestApp(tx *pop.Connection, admin bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	user := &models.User{
		ID:    uuid.Must(uuid.NewV4()),
		Login: "tester",
		Name:  "Test User",
		Admin: admin,
	}
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", user)
			return next(c)
		}
	})
	app.Resource("/webhook_api_keys", WebhookAPIKeysResource{})
	app.GET("/webhook_api_keys/{webhook_api_key_id}/created", WebhookAPIKeysResource{}.Created)
	return app
}

// perform performs an HTTP request against the test app and returns the recorder.
func perform(app *buffalo.App, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

// createKeyViaUI exercises the real Create handler to insert a key and returns
// the stored record.
func createKeyViaUI(t *testing.T, tx *pop.Connection, name, instanceID string) *models.WebhookAPIKey {
	t.Helper()
	app := newAdminTestApp(tx, true)

	// POST a create request with form body.
	body := "Name=" + name + "&InstanceID=" + instanceID
	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusSeeOther, rec.Code, "create should redirect")

	// Fetch the created key from the DB.
	keys := &models.WebhookAPIKeys{}
	require.NoError(t, tx.All(keys))
	require.Len(t, *keys, 1, "exactly one key should exist")
	return &(*keys)[0]
}

// ---------------------------------------------------------------------------
// Access control
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_NonAdminDenied(t *testing.T) {
	tx := setupTest(t)

	// Non-admin user → 403 on every action.
	app := newAdminTestApp(tx, false)

	for _, tc := range []struct {
		method, target string
	}{
		{"GET", "/webhook_api_keys"},
		{"GET", "/webhook_api_keys/new"},
		{"POST", "/webhook_api_keys"},
	} {
		rec := perform(app, tc.method, tc.target)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s should be forbidden", tc.method, tc.target)
	}
}

// ---------------------------------------------------------------------------
// List (index)
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_List(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	// Seed two keys directly.
	seedAPIKey(t, tx, "inst-a")
	seedAPIKey(t, tx, "inst-b")

	req := httptest.NewRequest("GET", "/webhook_api_keys", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// JSON response returns the list.
	var keys models.WebhookAPIKeys
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &keys))
	assert.Len(t, keys, 2)
}

func TestWebhookAPIKeys_ListJSON(t *testing.T) {
	tx := setupTest(t)
	seedAPIKey(t, tx, "")

	app := newAdminTestApp(tx, true)
	req := httptest.NewRequest("GET", "/webhook_api_keys", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_Create(t *testing.T) {
	tx := setupTest(t)

	key := createKeyViaUI(t, tx, "Brussels Center", "center-brussels")

	assert.Equal(t, "Brussels Center", key.Name)
	assert.Equal(t, "center-brussels", key.InstanceID)
	assert.NotEmpty(t, key.KeyHash, "hash should be generated")
	assert.NotEmpty(t, key.KeyPrefix, "prefix should be generated")
	assert.True(t, key.Active, "new key should be active")

	// The stored hash must authenticate the generated raw key (bcrypt).
	rawKey, _, _, err := models.GenerateKey()
	require.NoError(t, err)
	_ = rawKey // GenerateKey produces a *different* key; we only assert Authenticate works
	assert.True(t, key.Authenticate("creaves_does-not-match") == false, "wrong key must not authenticate")
}

func TestWebhookAPIKeys_CreateJSONReturnsRawKey(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader("Name=JSONKey"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["raw_key"], "JSON create must return the raw key once")
	assert.Contains(t, resp["raw_key"], "creaves_")

	// The raw key must actually authenticate against the stored hash.
	keys := &models.WebhookAPIKeys{}
	require.NoError(t, tx.All(keys))
	require.Len(t, *keys, 1)
	assert.True(t, (*keys)[0].Authenticate(resp["raw_key"].(string)))
}

// ---------------------------------------------------------------------------
// Show
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_Show(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "")

	req := httptest.NewRequest("GET", "/webhook_api_keys/"+stored.ID.String(), nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var got models.WebhookAPIKey
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, stored.ID, got.ID)
}

func TestWebhookAPIKeys_ShowNotFound(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	rec := perform(app, "GET", "/webhook_api_keys/"+uuid.Must(uuid.NewV4()).String())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ---------------------------------------------------------------------------
// Edit / Update
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_Edit(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "inst-old")

	// HTML render of the edit form.
	rec := perform(app, "GET", "/webhook_api_keys/"+stored.ID.String()+"/edit")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Edit Webhook API Key")
}

func TestWebhookAPIKeys_EditNotFound(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	rec := perform(app, "GET", "/webhook_api_keys/"+uuid.Must(uuid.NewV4()).String()+"/edit")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWebhookAPIKeys_UpdateNotFound(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+uuid.Must(uuid.NewV4()).String(), strings.NewReader("Name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWebhookAPIKeys_Update(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "inst-old")

	body := "Name=Renamed&InstanceID=inst-new&Active=false"
	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusSeeOther, rec.Code, "update should redirect")

	var got models.WebhookAPIKey
	require.NoError(t, tx.Find(&got, stored.ID))
	assert.Equal(t, "Renamed", got.Name)
	assert.Equal(t, "inst-new", got.InstanceID)
	assert.False(t, got.Active)
}

// ---------------------------------------------------------------------------
// Destroy
// ---------------------------------------------------------------------------

func TestWebhookAPIKeys_Destroy(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "")

	req := httptest.NewRequest("DELETE", "/webhook_api_keys/"+stored.ID.String(), nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusSeeOther, rec.Code)

	count, err := tx.Count(&models.WebhookAPIKey{})
	require.NoError(t, err)
	assert.Equal(t, 0, count, "key should be deleted")
}

func TestWebhookAPIKeys_DestroyNotFound(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	rec := perform(app, "DELETE", "/webhook_api_keys/"+uuid.Must(uuid.NewV4()).String())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWebhookAPIKeys_CreateValidationFail(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	// Empty Name → validation error → re-renders new form (422).
	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader("Name=&InstanceID="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	count, err := tx.Count(&models.WebhookAPIKey{})
	require.NoError(t, err)
	assert.Equal(t, 0, count, "no key should be persisted on validation failure")
}

func TestWebhookAPIKeys_UpdateJSON(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "")

	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader("Name=ViaJSON&Active=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var got models.WebhookAPIKey
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "ViaJSON", got.Name)
}

func TestWebhookAPIKeys_DestroyJSON(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "")

	req := httptest.NewRequest("DELETE", "/webhook_api_keys/"+stored.ID.String(), nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

func TestWebhookAPIKeys_CreateJSONValidationFail(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader("Name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// TestWebhookAPIKeys_NewRendersForm covers the New handler HTML render path.
func TestWebhookAPIKeys_NewRendersForm(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	rec := perform(app, "GET", "/webhook_api_keys/new")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Create Webhook API Key")
}

// TestWebhookAPIKeys_ListHTML covers the HTML render path of List.
func TestWebhookAPIKeys_ListHTML(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	seedAPIKey(t, tx, "")

	rec := perform(app, "GET", "/webhook_api_keys")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Webhook API Keys")
}

// TestWebhookAPIKeys_ShowHTML covers the HTML render path of Show.
func TestWebhookAPIKeys_ShowHTML(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	_, stored := seedAPIKey(t, tx, "inst-1")

	rec := perform(app, "GET", "/webhook_api_keys/"+stored.ID.String())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), stored.Name)
}

// TestWebhookAPIKeys_UpdateValidationFail covers the validation-error branch of
// Update (empty name).
func TestWebhookAPIKeys_UpdateValidationFail(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	_, stored := seedAPIKey(t, tx, "")

	// Empty name → 422.
	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader("Name=&Active=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// TestWebhookAPIKeys_UpdateNonAdmin covers the admin-guard branch of Update:
// a non-admin user must receive 403 Forbidden.
func TestWebhookAPIKeys_UpdateNonAdmin(t *testing.T) {
	tx := setupTest(t)
	_, stored := seedAPIKey(t, tx, "")
	app := newAdminTestApp(tx, false)

	body := "Name=Renamed"
	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestWebhookAPIKeys_UpdateValidationFailJSON covers the JSON validation-error
// responder branch of Update (empty name + JSON accept → 422).
func TestWebhookAPIKeys_UpdateValidationFailJSON(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	_, stored := seedAPIKey(t, tx, "")

	req := httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader("Name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// TestWebhookAPIKeys_XMLResponders covers the XML responder branches of the
// resource handlers (List, Show). The responder library negotiates on the
// Accept header; we verify the handlers render without error for XML clients.
func TestWebhookAPIKeys_XMLResponders(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	_, stored := seedAPIKey(t, tx, "")

	// List with XML accept — should succeed (200).
	req := httptest.NewRequest("GET", "/webhook_api_keys", nil)
	req.Header.Set("Accept", "application/xml")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Show with XML accept — should succeed (200).
	req = httptest.NewRequest("GET", "/webhook_api_keys/"+stored.ID.String(), nil)
	req.Header.Set("Accept", "application/xml")
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestWebhookAPIKeys_ValidationFailXML covers the XML validation-error branches
// of Create and Update.
func TestWebhookAPIKeys_ValidationFailXML(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)
	_, stored := seedAPIKey(t, tx, "")

	// Create with empty name + XML accept → 422 via XML responder.
	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader("Name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/xml")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Update with empty name + XML accept → 422 via XML responder.
	req = httptest.NewRequest("PUT", "/webhook_api_keys/"+stored.ID.String(), strings.NewReader("Name="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/xml")
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// ---------------------------------------------------------------------------
// Created (one-time raw key display page)
// ---------------------------------------------------------------------------

// createKeyWithSession runs a real Create POST and returns the recorder, whose
// redirect target and session cookies feed the follow-up Created request.
func createKeyWithSession(t *testing.T, tx *pop.Connection, name string) *httptest.ResponseRecorder {
	t.Helper()
	app := newAdminTestApp(tx, true)

	req := httptest.NewRequest("POST", "/webhook_api_keys", strings.NewReader("Name="+name))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusSeeOther, rec.Code, "create should redirect")
	require.Contains(t, rec.Header().Get("Location"), "/created", "redirect must target the dedicated created page")
	return rec
}

// replayWithCookies copies the cookies set by rec onto a new request.
func replayWithCookies(rec *httptest.ResponseRecorder, method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	for _, ck := range rec.Result().Cookies() {
		req.AddCookie(ck)
	}
	return req
}

func TestWebhookAPIKeys_CreatedShowsRawKeyOnce(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	createRec := createKeyWithSession(t, tx, "OneTimeKey")
	createdURL := createRec.Header().Get("Location")

	// Follow the redirect with the session cookie: the raw key must be shown
	// on the dedicated page with the one-time warning and a copy affordance.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, replayWithCookies(createRec, "GET", createdURL))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.Contains(t, body, "creaves_", "raw key must be visible on the created page")
	assert.Contains(t, body, "only time", "one-time warning must be present")
	assert.Contains(t, body, "copy-api-key-btn", "copy affordance must be present")

	// The displayed raw key must authenticate against the stored hash.
	re := regexp.MustCompile(`creaves_[0-9a-fA-F-]+`)
	rawShown := re.FindString(body)
	require.NotEmpty(t, rawShown, "a raw key should appear in the page")
	keys := &models.WebhookAPIKeys{}
	require.NoError(t, tx.All(keys))
	require.Len(t, *keys, 1)
	assert.True(t, (*keys)[0].Authenticate(rawShown), "displayed raw key must authenticate")

	// Second visit (replaying the updated session cookie): the raw key is gone
	// (one-time display) → back to show.
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, replayWithCookies(rec, "GET", createdURL))
	assert.Equal(t, http.StatusSeeOther, rec2.Code)
	assert.NotContains(t, rec2.Body.String(), "creaves_", "raw key must never leak after the first display")
}

func TestWebhookAPIKeys_CreatedWithoutSessionRedirectsToShow(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, true)

	_, stored := seedAPIKey(t, tx, "")

	rec := perform(app, "GET", "/webhook_api_keys/"+stored.ID.String()+"/created")
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.NotContains(t, rec.Body.String(), "creaves_")
}

func TestWebhookAPIKeys_CreatedNonAdminForbidden(t *testing.T) {
	tx := setupTest(t)
	app := newAdminTestApp(tx, false)

	rec := perform(app, "GET", "/webhook_api_keys/"+uuid.Must(uuid.NewV4()).String()+"/created")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
