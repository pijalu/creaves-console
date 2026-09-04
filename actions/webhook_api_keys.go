package actions

import (
	"creaves-console/models"
	"fmt"
	"net/http"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/x/responder"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
)

// WebhookAPIKeysResource manages webhook API key configurations
type WebhookAPIKeysResource struct {
	buffalo.Resource
}

// List gets all WebhookAPIKeys
func (v WebhookAPIKeysResource) List(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	keys := &models.WebhookAPIKeys{}
	q := tx.PaginateFromParams(c.Params())

	if err := q.All(keys); err != nil {
		return err
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("pagination", q.Paginator)
		c.Set("webhookAPIKeys", keys)
		return c.Render(http.StatusOK, r.HTML("webhook_api_keys/index.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(keys))
	}).Respond(c)
}

// Show gets the data for one WebhookAPIKey
func (v WebhookAPIKeysResource) Show(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	key := &models.WebhookAPIKey{}
	if err := tx.Find(key, c.Param("webhook_api_key_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("webhookAPIKey", key)
		return c.Render(http.StatusOK, r.HTML("webhook_api_keys/show.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(key))
	}).Respond(c)
}

// New renders the form for creating a new WebhookAPIKey
func (v WebhookAPIKeysResource) New(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	c.Set("webhookAPIKey", &models.WebhookAPIKey{})
	return c.Render(http.StatusOK, r.HTML("webhook_api_keys/new.plush.html"))
}

// Create adds a WebhookAPIKey to the DB
func (v WebhookAPIKeysResource) Create(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	key := &models.WebhookAPIKey{}
	if err := c.Bind(key); err != nil {
		return errors.WithStack(err)
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	// Generate a new API key
	rawKey, hash, prefix, err := models.GenerateKey()
	if err != nil {
		return c.Error(http.StatusInternalServerError, fmt.Errorf("failed to generate key: %w", err))
	}

	key.ID = uuid.Must(uuid.NewV4())
	key.KeyHash = hash
	key.KeyPrefix = prefix
	key.KeyValue = nulls.NewString(rawKey)
	key.Active = true

	verrs, err := tx.ValidateAndCreate(key)
	if err != nil {
		return errors.WithStack(err)
	}

	if verrs.HasAny() {
		return responder.Wants("html", func(c buffalo.Context) error {
			c.Set("errors", verrs)
			c.Set("webhookAPIKey", key)
			return c.Render(http.StatusUnprocessableEntity, r.HTML("webhook_api_keys/new.plush.html"))
		}).Wants("json", func(c buffalo.Context) error {
			return c.Render(http.StatusUnprocessableEntity, r.JSON(verrs))
		}).Respond(c)
	}

	// Hand the raw key over to the dedicated one-time display page through the
	// session (never through the URL, which would leak into logs/history).
	c.Session().Set(rawKeySessionKey(key.ID), rawKey)
	if err := c.Session().Save(); err != nil {
		return errors.WithStack(err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/webhook_api_keys/%v/created", key.ID)
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusCreated, r.JSON(map[string]interface{}{
			"id":         key.ID,
			"name":       key.Name,
			"key_prefix": key.KeyPrefix,
			"raw_key":    rawKey, // Only shown once on creation
		}))
	}).Respond(c)
}

// rawKeySessionKey is the session key under which the raw API key is stored
// between Create (POST) and the dedicated one-time display page (GET).
func rawKeySessionKey(id uuid.UUID) string {
	return "raw_api_key_" + id.String()
}

// Created shows the raw API key exactly once, right after Create. The raw key
// is passed through the session (never through the URL) and removed from the
// session as soon as it has been displayed.
func (v WebhookAPIKeysResource) Created(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	key := &models.WebhookAPIKey{}
	if err := tx.Find(key, c.Param("webhook_api_key_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	sessKey := rawKeySessionKey(key.ID)
	raw := c.Session().Get(sessKey)
	if raw == nil {
		// Already displayed (or unknown navigation): the raw key is gone for good.
		c.Flash().Add("warning", "The raw API key is no longer available. It is only shown once, right after creation.")
		return c.Redirect(http.StatusSeeOther, "/webhook_api_keys/%v", key.ID)
	}
	c.Session().Delete(sessKey)
	if err := c.Session().Save(); err != nil {
		return errors.WithStack(err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("webhookAPIKey", key)
		c.Set("rawKey", raw)
		return c.Render(http.StatusOK, r.HTML("webhook_api_keys/created.plush.html"))
	}).Respond(c)
}

// Edit renders a edit form for a WebhookAPIKey
func (v WebhookAPIKeysResource) Edit(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	key := &models.WebhookAPIKey{}
	if err := tx.Find(key, c.Param("webhook_api_key_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	c.Set("webhookAPIKey", key)
	return c.Render(http.StatusOK, r.HTML("webhook_api_keys/edit.plush.html"))
}

// Update changes a WebhookAPIKey in the DB
func (v WebhookAPIKeysResource) Update(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	key := &models.WebhookAPIKey{}
	if err := tx.Find(key, c.Param("webhook_api_key_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	if err := c.Bind(key); err != nil {
		return errors.WithStack(err)
	}

	verrs, err := tx.ValidateAndUpdate(key)
	if err != nil {
		return errors.WithStack(err)
	}

	if verrs.HasAny() {
		return responder.Wants("html", func(c buffalo.Context) error {
			c.Set("errors", verrs)
			c.Set("webhookAPIKey", key)
			return c.Render(http.StatusUnprocessableEntity, r.HTML("webhook_api_keys/edit.plush.html"))
		}).Wants("json", func(c buffalo.Context) error {
			return c.Render(http.StatusUnprocessableEntity, r.JSON(verrs))
		}).Respond(c)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Flash().Add("success", "API Key updated successfully")
		return c.Redirect(http.StatusSeeOther, "/webhook_api_keys/%v", key.ID)
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusOK, r.JSON(key))
	}).Respond(c)
}

// Destroy deletes a WebhookAPIKey from the DB
func (v WebhookAPIKeysResource) Destroy(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	key := &models.WebhookAPIKey{}
	if err := tx.Find(key, c.Param("webhook_api_key_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	if err := tx.Destroy(key); err != nil {
		return errors.WithStack(err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Flash().Add("success", "API Key deleted successfully")
		return c.Redirect(http.StatusSeeOther, "/webhook_api_keys")
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusOK, r.JSON(key))
	}).Respond(c)
}
