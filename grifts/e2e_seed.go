package grifts

import (
	"fmt"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/grift/grift"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"golang.org/x/crypto/bcrypt"
)

// E2E fixture seed for the console (plan §7.2).
//
// The console is fed two ways in the e2e environment:
//
//   - Instance A ("e2e-instance-a") rows arrive through the REAL webhook path:
//     the Creaves app (seeded with its own db:seed:e2e) emits animal_state
//     resync events to http://127.0.0.1:3001/webhook/events authenticated with
//     the raw key E2EConsoleKey below.
//   - Instance B ("e2e-instance-b") rows are seeded directly by this task so
//     the scope selector has two known instances with different, fixed numbers
//     (see creaves/e2e/EXPECTATIONS.md).
//
// This task therefore creates:
//   - the webhook API key (bcrypt hash of the fixed raw key E2EConsoleKey)
//   - the "e2e-instance-b" CreavesInstance row
//   - 6 consolidated_animals rows for instance B (2025 x5 incl. one fully
//     NULL-category row and one outtake-dead row, 2024 x1), with fixed
//     E2EB_-prefixed values including a city containing ';' and quotes for CSV
//     escaping assertions.

// E2EConsoleKey must match the creaves fixture (grifts/e2e_seed.go).
const E2EConsoleKey = "e2e-console-key-0123456789"

// E2EInstanceB is the directly seeded second center.
const E2EInstanceB = "e2e-instance-b"

func e2eBNull(s string) nulls.String {
	if s == "" {
		return nulls.String{}
	}
	return nulls.NewString(s)
}

var _ = grift.Namespace("db", func() {
	grift.Desc("seed:e2e", "Seeds fixed E2E fixture data (plan §7.2): webhook key + instance B rows")
	grift.Add("seed:e2e", func(c *grift.Context) error {
		return models.DB.Transaction(func(tx *pop.Connection) error {
			if exists, err := tx.Where("instance_id = ?", E2EInstanceB).Exists(&models.ConsolidatedAnimal{}); err != nil {
				return err
			} else if exists {
				return fmt.Errorf("E2E fixtures already present; reset the database first (CONFIRM=cleanup buffalo task db:cleanup)")
			}

			now := time.Now()

			// 1. Webhook API key (raw key known only to the creaves fixture config)
			if exists, err := tx.Where("name = ?", "e2e-key").Exists(&models.WebhookAPIKey{}); err != nil {
				return err
			} else if !exists {
				hash, err := bcrypt.GenerateFromPassword([]byte(E2EConsoleKey), bcrypt.DefaultCost)
				if err != nil {
					return err
				}
				key := &models.WebhookAPIKey{
					ID:        uuid.Must(uuid.NewV4()),
					Name:      "e2e-key",
					KeyHash:   string(hash),
					KeyPrefix: E2EConsoleKey[:12],
					Active:    true,
				}
				if err := tx.Create(key); err != nil {
					return err
				}
			}

			// 2. Instance B registration
			if err := models.UpsertByInstanceID(tx, E2EInstanceB, "E2E Center B", "Directly seeded e2e fixture center", now, nil); err != nil {
				return err
			}

			// 3. Instance B consolidated rows.
			type row struct {
				animalID, year, yearNumber int
				species, class, agw        string
				subside, native            string
				age, atype                 string
				ecCause, ecDetail, ecNat   string
				ring, city                 string
				outtakeType                string
				rating                     int
				dead                       bool
			}
			rows := []row{
				{950001, 2025, 1, "E2EB_Fox", "E2EB_Mammalia", "E2EB_G1", "E2EB_SGB", "E2EB_NSB", "E2EB Adult", "E2EB_TA", "E2EB_C1", "E2EB_D1", "E2EB_N1", "E2EB-RING-001", "E2EB Town", "E2EB_REL", 1, false},
				{950002, 2025, 2, "E2EB_Fox", "E2EB_Mammalia", "E2EB_G1", "E2EB_SGB", "E2EB_NSB", "E2EB Adult", "E2EB_TA", "E2EB_C1", "E2EB_D1", "E2EB_N1", "E2EB-RING-002", "E2EB Town", "", 0, false},
				{950003, 2025, 3, "E2EB_Owl", "E2EB_Aves", "E2EB_G2", "", "", "E2EB Juvenile", "E2EB_TB", "E2EB_C2", "", "E2EB_N2", "", `E2EB City; "Sud"`, "", 0, false},
				{950004, 2025, 4, "E2EB_Owl", "E2EB_Aves", "E2EB_G2", "", "", "E2EB Juvenile", "E2EB_TB", "E2EB_C2", "", "E2EB_N2", "E2EB-RING-004", "E2EB Village", "E2EB_DCD", -1, true},
				{950005, 2025, 5, "", "", "", "", "", "", "", "", "", "", "", "", "", 0, false},
				{940006, 2024, 6, "E2EB_Fox", "E2EB_Mammalia", "E2EB_G1", "E2EB_SGB", "E2EB_NSB", "E2EB Adult", "E2EB_TA", "E2EB_C1", "E2EB_D1", "E2EB_N1", "E2EB-RING-006", "E2EB Town", "E2EB_REL", 1, false},
			}
			for _, r := range rows {
				ca := &models.ConsolidatedAnimal{
					ID:                  uuid.Must(uuid.NewV4()),
					InstanceID:          E2EInstanceB,
					AnimalID:            r.animalID,
					Year:                r.year,
					YearNumber:          r.yearNumber,
					Species:             e2eBNull(r.species),
					SpeciesClass:        e2eBNull(r.class),
					SpeciesAGWGroup:     e2eBNull(r.agw),
					SpeciesSubsideGroup: e2eBNull(r.subside),
					SpeciesNativeStatus: e2eBNull(r.native),
					AnimalAge:           e2eBNull(r.age),
					AnimalType:          e2eBNull(r.atype),
					EntryCause:          e2eBNull(r.ecCause),
					EntryCauseDetail:    e2eBNull(r.ecDetail),
					EntryCauseNature:    e2eBNull(r.ecNat),
					Ring:                e2eBNull(r.ring),
					DiscoveryCity:       e2eBNull(r.city),
					CurrentStatus:       "in_care",
					LastEventAt:         now,
					EventCount:          1,
				}
				if r.outtakeType != "" {
					ca.OuttakeType = nulls.NewString(r.outtakeType)
					ca.OuttakeRating = nulls.NewInt(r.rating)
					ca.OuttakeDead = nulls.NewBool(r.dead)
					ca.CurrentStatus = "released"
				}
				if err := tx.Create(ca); err != nil {
					return err
				}
			}

			fmt.Println("E2E console fixtures seeded: webhook key 'e2e-key', instance e2e-instance-b with 6 consolidated animals (2025 x5, 2024 x1)")
			return nil
		})
	})
})
