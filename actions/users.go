package actions

import (
	"creaves-console/models"
	"fmt"
	"net/http"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/x/responder"
	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

// AuthLanding default implementation.
func AuthLanding(c buffalo.Context) error {
	return c.Render(http.StatusOK, r.HTML("auth/landing.plush.html"))
}

// AuthNew loads the signin page
func AuthNew(c buffalo.Context) error {
	c.Set("user", models.User{})
	return c.Render(http.StatusOK, r.HTML("auth/new.plush.html"))
}

// AuthCreate attempts to log the user in with an existing account.
func AuthCreate(c buffalo.Context) error {
	u := &models.User{}
	if err := c.Bind(u); err != nil {
		return errors.WithStack(err)
	}

	tx := c.Value("tx").(*pop.Connection)

	// find a user with the login
	err := tx.Where("login = ?", u.Login).First(u)

	// helper function to handle bad attempts
	bad := func() error {
		c.Set("user", u)
		c.Flash().Add("danger", "Invalid login or password")
		return c.Render(http.StatusUnauthorized, r.HTML("auth/new.plush.html"))
	}

	if err != nil {
		// couldn't find an user with the supplied login address.
		return bad()
	}

	// confirm that the given password matches the hashed password from the db
	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(u.Password))
	if err != nil {
		return bad()
	}

	// check if user is active
	if !u.Active {
		c.Flash().Add("danger", "Account is disabled")
		return c.Render(http.StatusUnauthorized, r.HTML("auth/new.plush.html"))
	}

	c.Session().Set("current_user_id", u.ID)
	c.Flash().Add("success", "Welcome back!")

	redirectURL := "/"
	if c.Session().Get("redirectURL") != nil {
		redirectURL = c.Session().Get("redirectURL").(string)
	}

	return c.Redirect(302, redirectURL)
}

// AuthDestroy clears the session and logs a user out
func AuthDestroy(c buffalo.Context) error {
	c.Session().Clear()
	c.Flash().Add("success", "You have been logged out")
	return c.Redirect(302, "/auth/new")
}

// SetCurrentUser attempts to find a user based on the current_user_id
// in the session. If one is found it is set on the context.
func SetCurrentUser(next buffalo.Handler) buffalo.Handler {
	return func(c buffalo.Context) error {
		if uid := c.Session().Get("current_user_id"); uid != nil {
			u := &models.User{}
			tx := c.Value("tx").(*pop.Connection)
			err := tx.Find(u, uid)
			if err != nil {
				c.Session().Delete("current_user_id")
				c.Session().Set("redirectURL", c.Request().URL.String())
				return next(c)
			}
			if !u.Active {
				c.Session().Clear()
				c.Flash().Add("danger", "Your account is no longer active")
				return c.Redirect(302, "/auth/new")
			}
			c.Set("current_user", u)
		}
		return next(c)
	}
}

// Authorize require a user be logged in before accessing a route
func Authorize(next buffalo.Handler) buffalo.Handler {
	return func(c buffalo.Context) error {
		if uid := c.Session().Get("current_user_id"); uid == nil {
			c.Session().Set("redirectURL", c.Request().URL.String())

			err := c.Session().Save()
			if err != nil {
				return errors.WithStack(err)
			}

			c.Flash().Add("danger", "You must be logged in to access this page")
			return c.Redirect(302, "/auth/new")
		}
		return next(c)
	}
}

// GetCurrentUser retrieve user from middleware
func GetCurrentUser(c buffalo.Context) *models.User {
	cu := c.Value("current_user")
	if cu == nil {
		return nil
	}
	return cu.(*models.User)
}

// UsersResource is the resource for the User model
type UsersResource struct {
	buffalo.Resource
}

// List gets all Users
func (v UsersResource) List(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	users := &models.Users{}
	q := tx.PaginateFromParams(c.Params())

	if err := q.All(users); err != nil {
		return err
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("pagination", q.Paginator)
		c.Set("users", users)
		return c.Render(http.StatusOK, r.HTML("users/index.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(users))
	}).Respond(c)
}

// Show gets the data for one User
func (v UsersResource) Show(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || (!cu.Admin && cu.ID.String() != c.Param("user_id")) {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	user := &models.User{}
	if err := tx.Find(user, c.Param("user_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("user", user)
		return c.Render(http.StatusOK, r.HTML("users/show.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(user))
	}).Respond(c)
}

// New renders the form for creating a new User
func (v UsersResource) New(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	c.Set("user", &models.User{})
	return c.Render(http.StatusOK, r.HTML("users/new.plush.html"))
}

// Create adds a User to the DB
func (v UsersResource) Create(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	user := &models.User{}
	if err := c.Bind(user); err != nil {
		return errors.WithStack(err)
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	verrs, err := user.Create(tx)
	if err != nil {
		return errors.WithStack(err)
	}

	if verrs.HasAny() {
		return responder.Wants("html", func(c buffalo.Context) error {
			c.Set("errors", verrs)
			c.Set("user", user)
			return c.Render(http.StatusUnprocessableEntity, r.HTML("users/new.plush.html"))
		}).Wants("json", func(c buffalo.Context) error {
			return c.Render(http.StatusUnprocessableEntity, r.JSON(verrs))
		}).Respond(c)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Flash().Add("success", "User created successfully")
		return c.Redirect(http.StatusSeeOther, "/users/%v", user.ID)
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusCreated, r.JSON(user))
	}).Respond(c)
}

// Edit renders a edit form for a User
func (v UsersResource) Edit(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || (!cu.Admin && cu.ID.String() != c.Param("user_id")) {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	user := &models.User{}
	if err := tx.Find(user, c.Param("user_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	c.Set("user", user)
	return c.Render(http.StatusOK, r.HTML("users/edit.plush.html"))
}

// Update changes a User in the DB
func (v UsersResource) Update(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || (!cu.Admin && cu.ID.String() != c.Param("user_id")) {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	user := &models.User{}
	if err := tx.Find(user, c.Param("user_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	if err := c.Bind(user); err != nil {
		return errors.WithStack(err)
	}

	if len(user.Password) > 0 {
		if err := user.SetPasswordHash(); err != nil {
			return err
		}
	}

	verrs, err := tx.ValidateAndUpdate(user)
	if err != nil {
		return errors.WithStack(err)
	}

	if verrs.HasAny() {
		return responder.Wants("html", func(c buffalo.Context) error {
			c.Set("errors", verrs)
			c.Set("user", user)
			return c.Render(http.StatusUnprocessableEntity, r.HTML("users/edit.plush.html"))
		}).Wants("json", func(c buffalo.Context) error {
			return c.Render(http.StatusUnprocessableEntity, r.JSON(verrs))
		}).Respond(c)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Flash().Add("success", "User updated successfully")
		return c.Redirect(http.StatusSeeOther, "/users/%v", user.ID)
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusOK, r.JSON(user))
	}).Respond(c)
}

// Destroy deletes a User from the DB
func (v UsersResource) Destroy(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("Admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	user := &models.User{}
	if err := tx.Find(user, c.Param("user_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	if err := tx.Destroy(user); err != nil {
		return errors.WithStack(err)
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Flash().Add("success", "User deleted successfully")
		return c.Redirect(http.StatusSeeOther, "/users")
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(http.StatusOK, r.JSON(user))
	}).Respond(c)
}
