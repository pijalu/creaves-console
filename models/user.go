package models

import (
	"encoding/json"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gobuffalo/validate/v3/validators"
	"github.com/gofrs/uuid"
	"golang.org/x/crypto/bcrypt"
	"github.com/pkg/errors"
)

// User represents an authority user for the consolidation app
type User struct {
	ID           uuid.UUID `json:"id" db:"id"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
	Login        string    `json:"login" db:"login" form:"login"`
	Name         string    `json:"name" db:"name" form:"name"`
	Email        string    `json:"email" db:"email" form:"email"`
	Admin        bool      `json:"admin" db:"admin" form:"admin"`
	Active       bool      `json:"active" db:"active" form:"active"`
	PasswordHash string    `json:"password_hash" db:"password_hash"`

	Password             string `json:"-" db:"-" form:"password"`
	PasswordConfirmation string `json:"-" db:"-" form:"password_confirmation"`
}

func (u *User) SetPasswordHash() error {
	ph, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return errors.WithStack(err)
	}
	u.PasswordHash = string(ph)
	return nil
}

func (u *User) Create(tx *pop.Connection) (*validate.Errors, error) {
	err := u.SetPasswordHash()
	if err != nil {
		return validate.NewErrors(), errors.WithStack(err)
	}
	return tx.ValidateAndCreate(u)
}

func (u User) String() string {
	ju, _ := json.Marshal(u)
	return string(ju)
}

type Users []User

func (u Users) String() string {
	ju, _ := json.Marshal(u)
	return string(ju)
}

func (u *User) Validate(tx *pop.Connection) (*validate.Errors, error) {
	var err error
	return validate.Validate(
		&validators.StringIsPresent{Field: u.Login, Name: "Login"},
		&validators.StringIsPresent{Field: u.Name, Name: "Name"},
		&validators.StringIsPresent{Field: u.PasswordHash, Name: "PasswordHash"},
		&validators.FuncValidator{
			Field:   u.Login,
			Name:    "Login",
			Message: "%s is already taken",
			Fn: func() bool {
				var b bool
				q := tx.Where("login = ?", u.Login)
				if u.ID != uuid.Nil {
					q = q.Where("id != ?", u.ID)
				}
				b, err = q.Exists(u)
				if err != nil {
					return false
				}
				return !b
			},
		},
	), err
}

func (u *User) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	var err error
	return validate.Validate(
		&validators.StringIsPresent{Field: u.Password, Name: "Password"},
		&validators.StringsMatch{Name: "Password", Field: u.Password, Field2: u.PasswordConfirmation, Message: "Password does not match confirmation"},
	), err
}

func (u *User) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	var err error
	return validate.Validate(
		&validators.StringsMatch{Name: "Password", Field: u.Password, Field2: u.PasswordConfirmation, Message: "Password does not match confirmation"},
	), err
}
