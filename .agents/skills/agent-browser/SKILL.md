---
name: agent-browser
description: Test the Creaves Console web app with agent-browser. Use when validating pages, forms, language variants (en-US/de/nl), or running E2E checks on the consolidation app. Covers login, language switching via the lang cookie, navigation, and checking for template errors / 500s. Triggers include "test console", "validate console pages", "check language variants", "E2E creaves-console".
category: action
inline: false
mode: coder
temperature: 0.2
tools:
  - Bash(agent-browser:*)
  - Bash(npx agent-browser:*)
---

# Creaves Console testing with agent-browser

Fast browser automation CLI for AI agents (Chrome via CDP, accessibility-tree
snapshots with `@eN` refs). Use this skill for all Creaves Console UI
validation.

## App facts

- **URL**: `http://127.0.0.1:3001`
- **Login**: `admin` / `admin123`
- **Language switching**: cookie `lang` — values `en-US`, `de`, `nl`.
  Console default is `en-US`; `*.plush.de.html` / `*.plush.nl.html`
  variants are resolved per language with fallback to base English.
- **Dev server**: `buffalo dev` from `creaves-console/`.

## Core workflow

```bash
agent-browser open http://127.0.0.1:3001/auth/new
agent-browser snapshot -i            # get refs like @e1, @e2
agent-browser fill @e1 "admin"       # login
agent-browser fill @e2 "admin123"
agent-browser click @e3              # submit
agent-browser wait --load networkidle
agent-browser snapshot -i            # re-snapshot after every navigation
```

The browser persists between commands (background daemon). Refs are stale
after any page change — always re-snapshot before interacting again.

## Language switching

The `/lang/?lang=de&url=...` (or `nl`) route sets the `lang` cookie. To
force a language directly, set the cookie before loading a page:

```bash
agent-browser open http://127.0.0.1:3001/
agent-browser cookie set lang de
agent-browser open http://127.0.0.1:3001/dashboard
agent-browser snapshot -i
```

(or use the in-app language dropdown in the navbar).

## Testing checklist (de/nl variant validation)

For each language (`de`, `nl`, plus `en-US` regression), verify:

1. **Auth**: login page renders; login succeeds with `admin`/`admin123`.
2. **Dashboard**: `/dashboard` loads, HTTP 200, no template error in log.
3. **Each page** spot-check: `/consolidated_animals`,
   `/consolidated_animals/:id`, `/reports`, `/reports/by_location`,
   `/reports/by_type`, `/reports/by_species`, `/webhook_api_keys`,
   `/webhook_api_keys/new`.
4. **No hardcoded English in de/nl output**: snapshot visible text and
   confirm headings/buttons/labels are German (de) or Dutch (nl).
5. **Admin actions**: webhook API key list loads for admin; users page
   loads; no 500s.
6. **No 404/500**: every visited URL returns 200; server log shows no
   template resolution errors.

## Full command reference

For anything beyond this workflow (screenshots, sessions, parallel runs,
auth state), load the canonical skill:

```bash
agent-browser skills get core --full
```
