package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDashboardTemplatesStatCardParity guards against locale template drift:
// every localized dashboard variant must render the same stat cards as the
// en-US master (bugs.md item 10).
func TestDashboardTemplatesStatCardParity(t *testing.T) {
	masterKeys := dashboardStatCardKeys(t, filepath.Join("..", "templates", "dashboard", "index.plush.html"))
	if len(masterKeys) == 0 {
		t.Fatal("en-US dashboard template has no stat cards")
	}

	for _, lang := range []string{"fr", "de", "nl"} {
		path := filepath.Join("..", "templates", "dashboard", "index.plush."+lang+".html")
		got := dashboardStatCardKeys(t, path)
		if len(got) != len(masterKeys) {
			t.Errorf("%s template renders %d stat cards, en-US master has %d", lang, len(got), len(masterKeys))
			continue
		}
		for i, key := range masterKeys {
			if got[i] != key {
				t.Errorf("%s stat card %d renders %q, want %q", lang, i, got[i], key)
			}
		}
	}
}

// dashboardStatCardKeys returns the stats map keys rendered inside stat-card
// blocks, in document order.
func dashboardStatCardKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var keys []string
	inCard := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, `class="card stat-card"`) {
			inCard = true
			continue
		}
		if !inCard {
			continue
		}
		if idx := strings.Index(line, `stats["`); idx >= 0 {
			rest := line[idx+len(`stats["`):]
			if end := strings.Index(rest, `"]`); end >= 0 {
				keys = append(keys, rest[:end])
			}
			inCard = false
		} else if strings.Contains(line, "</div>") && !strings.Contains(line, "card-body") {
			// Left the card body without finding a stats expression.
			inCard = false
		}
	}
	return keys
}
