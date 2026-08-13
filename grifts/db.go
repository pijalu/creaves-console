package grifts

import (
	"creaves-console/models"
	"fmt"

	"github.com/gobuffalo/grift/grift"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"golang.org/x/crypto/bcrypt"
)

var _ = grift.Namespace("db", func() {
	grift.Desc("seed", "Seeds the consolidation database")
	grift.Add("seed", func(c *grift.Context) error {
		return createAdminUser(c)
	})
})

func createAdminUser(c *grift.Context) error {
	return models.DB.Transaction(func(tx *pop.Connection) error {
		// Check if admin already exists
		exists, err := tx.Where("login = ?", "admin").Exists(&models.User{})
		if err != nil {
			return err
		}
		if exists {
			fmt.Println("Admin user already exists")
			return nil
		}

		// Create admin user
		password := "admin123" // Change this in production!
		ph, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		admin := &models.User{
			ID:           uuid.Must(uuid.NewV4()),
			Login:        "admin",
			Name:         "Administrator",
			Email:        "admin@consolidation.local",
			Admin:        true,
			Active:       true,
			PasswordHash: string(ph),
		}

		if err := tx.Create(admin); err != nil {
			return err
		}

		fmt.Printf("Admin user created: login=admin, password=%s\n", password)
		fmt.Println("WARNING: Please change the default password!")
		return nil
	})
}
