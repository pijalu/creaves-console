package actions

import (
	"creaves-console/models"
	"creaves-console/public"
	"creaves-console/templates"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/gobuffalo/buffalo/render"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/plush/v4"
)

// uiLanguages lists all selectable UI languages (cookie value, native label).
// The base/canonical language of the console is en-US: plain templates are
// the English versions, .plush.de.html and .plush.nl.html are the variants.
var uiLanguages = []struct {
	code  string
	label string
}{
	{"en-US", "English"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"nl", "Nederlands"},
}

// langLinks renders one link per UI language, except the current one.
// linkClass "nav-link" wraps each link in a <li class="nav-item"> for the
// anonymous navbar; anything else renders plain dropdown-item anchors.
// Target URL is preserved through the /lang switcher so the user stays on
// the same page after changing language (mirrors the creaves pattern).
func langLinks(target any, linkClass string, help plush.HelperContext) (template.HTML, error) {
	targetURL := fmt.Sprintf("%v", target)
	cur := ""
	if req, ok := help.Value("request").(*http.Request); ok {
		if cookie, err := req.Cookie("lang"); err == nil {
			cur = normalizeUILang(cookie.Value)
		}
	}
	var b strings.Builder
	for _, l := range uiLanguages {
		code := l.code
		norm := code
		if code == "en-US" {
			norm = "en-US" // base/canonical English
		}
		if norm == cur {
			continue
		}
		href := fmt.Sprintf("/lang/?lang=%s&url=%s", code, url.QueryEscape(targetURL))
		if linkClass == "nav-link" {
			fmt.Fprintf(&b, `<li class="nav-item"><a class="nav-link" href="%s">%s</a></li>`, href, l.label)
		} else {
			fmt.Fprintf(&b, `<a class="dropdown-item" href="%s">%s</a>`, href, l.label)
		}
	}
	return template.HTML(b.String()), nil
}

// normalizeUILang maps a lang cookie value to the comparison domain used by
// langLinks: "en-US" for the base/canonical English console, otherwise the
// full code.
func normalizeUILang(lang string) string {
	switch lang {
	case "", "en", "en-US":
		return "en-US"
	default:
		return lang
	}
}

// currentUILang returns request-selected language, defaulting to canonical English.
func currentUILang(help plush.HelperContext) string {
	if req, ok := help.Value("request").(*http.Request); ok {
		if cookie, err := req.Cookie("lang"); err == nil && cookie.Value != "" {
			return normalizeUILang(cookie.Value)
		}
	}
	return "en-US"
}

// statusLabels maps internal status codes to UI labels per language.
var statusLabels = map[string]map[string]string{
	"in_care": {
		"en-US": "In care",
		"fr":    "En soins",
		"de":    "In Pflege",
		"nl":    "In verzorging",
	},
	"released": {
		"en-US": "Released",
		"fr":    "Relâché",
		"de":    "Freigelassen",
		"nl":    "Vrijgelaten",
	},
	"died": {
		"en-US": "Died",
		"fr":    "Décédé",
		"de":    "Verstorben",
		"nl":    "Overleden",
	},
}

// localizedStatus renders an internal status code (in_care/released/died)
// as a human-readable label in the current UI language. Unknown values are
// returned unchanged. Accepts string or nulls.String.
func localizedStatus(value interface{}, help plush.HelperContext) (string, error) {
	var status string
	switch v := value.(type) {
	case string:
		status = v
	case nulls.String:
		if !v.Valid {
			return "", nil
		}
		status = v.String
	default:
		status = fmt.Sprintf("%v", value)
	}
	if labels, ok := statusLabels[status]; ok {
		if label, ok := labels[currentUILang(help)]; ok && label != "" {
			return label, nil
		}
		if label, ok := labels["en-US"]; ok {
			return label, nil
		}
	}
	return status, nil
}

func localizedLabel(value string, labels map[string]string, help plush.HelperContext) (string, error) {
	if label, ok := labels[value]; ok && label != "" {
		return label, nil
	}
	return value, nil
}

func localizedField(value interface{}, field string, help plush.HelperContext) (string, error) {
	lang := currentUILang(help)
	switch labels := value.(type) {
	case map[string]string:
		return labels[field], nil
	}
	switch animal := value.(type) {
	case models.ConsolidatedAnimal:
		return animal.LocalizedField(lang, field), nil
	case *models.ConsolidatedAnimal:
		if animal == nil {
			return "", nil
		}
		return animal.LocalizedField(lang, field), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}

// csrfToken resolves the CSRF token set by the csrf middleware (or empty
// when rendering without it, e.g. in minimal unit-test apps), so forms in
// the layout can include the hidden authenticity_token input.
// Registered under the name "csrf_token": mw-csrf itself stores the masked
// token string under "authenticity_token" on the buffalo context, and that
// key would otherwise collide with the helper function (buffalo merges the
// helper map over the context data, so a bare <%= authenticity_token %>
// would resolve to the function object and render as an empty string).
func csrfToken(help plush.HelperContext) string {
	if tok, ok := help.Value("authenticity_token").(string); ok {
		return tok
	}
	return ""
}

var r *render.Engine

func init() {
	r = render.New(render.Options{
		HTMLLayout:  "application.plush.html",
		TemplatesFS: templates.FS(),
		AssetsFS:    public.FS(),
		Helpers: render.Helpers{
			"bool2html": func(s bool) string {
				if s {
					return "✓"
				}
				return "✗"
			},
			"langLinks":         langLinks,
			"tfield_localized":  localizedField,
			"tlabel_localized":  localizedLabel,
			"tstatus_localized": localizedStatus,
			"csrf_token":        csrfToken,
			"sortLink":          sortLink,
			"sortIcon":          sortIcon,
			"outcomeClass":      outcomeClass,
			"outcomeLabel":      outcomeLabel,
		},
	})
}
