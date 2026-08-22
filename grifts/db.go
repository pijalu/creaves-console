package grifts

import (
	"creaves-console/models"
	"fmt"
	"os"

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
	grift.Desc("cleanup", "Deletes all application data (requires CONFIRM=cleanup)")
	grift.Add("cleanup", func(c *grift.Context) error {
		return cleanupDatabase()
	})
})

func cleanupDatabase() error {
	if os.Getenv("CONFIRM") != "cleanup" {
		return fmt.Errorf("refusing to clean database: set CONFIRM=cleanup explicitly")
	}

	// Keep schema_migration intact so this task removes data without damaging the
	// migration history. Delete dependent records first for FK-enabled databases.
	tables := []string{
		"event_streams",
		"consolidated_animals",
		"import_runs",
		"creaves_instances",
		"webhook_api_keys",
		"users",
	}

	return models.DB.Transaction(func(tx *pop.Connection) error {
		for _, table := range tables {
			if err := tx.RawQuery("DELETE FROM " + table).Exec(); err != nil {
				return fmt.Errorf("failed to clean %s: %w", table, err)
			}
		}
		fmt.Printf("Cleaned application data from %d tables (schema_migration preserved)\n", len(tables))
		return nil
	})
}

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
