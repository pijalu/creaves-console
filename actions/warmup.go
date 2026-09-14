package actions

import (
	"log"
	"time"

	"creaves-console/models"
)

// warmupUILangs mirrors the selectable UI languages (render.go statusLabels
// set): the register label caches are per language.
var warmupUILangs = []string{"en-US", "fr", "de", "nl"}

// warmupCaches pre-builds the register reference caches (refcache.go) in the
// background so the first user after boot does not pay the cold-cache cost
// (7 DISTINCT scans + 5 grouped label queries, registerFilterOptions).
// Failures are logged and ignored: every builder runs lazily on first use.
func warmupCaches() {
	// Let the HTTP server come up first; the warmup must not race migrations
	// or block boot on a slow database.
	time.Sleep(2 * time.Second)

	start := time.Now()
	if models.DB == nil {
		return
	}
	scope := ResolveReportScope("")
	for _, lang := range warmupUILangs {
		if _, err := registerFilterOptions(models.DB, scope, lang); err != nil {
			log.Printf("cache warmup (lang %s): %v", lang, err)
		}
	}
	if _, err := consolidatedInstanceOptions(models.DB); err != nil {
		log.Printf("cache warmup (instances): %v", err)
	}
	log.Printf("cache warmup done in %s", time.Since(start))
}
