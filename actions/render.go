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
			"langLinks":        langLinks,
			"tfield_localized": localizedField,
			"tlabel_localized": localizedLabel,
		},
	})
}
