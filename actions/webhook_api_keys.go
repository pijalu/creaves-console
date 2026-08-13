package actions

import (
	"creaves-console/models"
	"fmt"
	"net/http"

	"github.com/gobuffalo/buffalo"
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

	// Store raw key in flash to show once
	c.Flash().Add("success", fmt.Sprintf("API Key created successfully. Your key is: %s (copy it now - it will not be shown again)", rawKey))

	return responder.Wants("html", func(c buffalo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/webhook_api_keys/%v", key.ID)
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusCreated, r.JSON(map[string]interface{}{
			"id":         key.ID,
			"name":       key.Name,
			"key_prefix": key.KeyPrefix,
			"raw_key":    rawKey, // Only shown once on creation
		}))
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
