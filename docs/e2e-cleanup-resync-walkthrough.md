# E2E walkthrough — console cleanup + Creaves full resync (recovery path)

This is the repeatable operator walkthrough for the paired flows:

- **Creaves Console** (admin): *Instances → Delete* — purges one source
  instance's events, consolidated animals, restricted API keys and registry
  row (`POST /instances/{id}/cleanup`, requires typing the instance_id).
- **Creaves** (admin): *Webhook resync* — sends the current animal state to
  the configured console. Normal runs skip unchanged animals; with
  **Force full rebuild** every state event is re-queued (deterministic event
  IDs make the console-side rebuild idempotent).

Automated coverage: `creaves-console/actions/webhook_e2e_test.go`
(`TestE2E_V2CleanupThenResync`, `TestE2E_V2Resync_IdempotentAndLocalized`)
and `creaves/actions/webhook_resync_test.go`
(`TestRunResyncForceRequeuesDeliveredEvents`).

## Prerequisites

1. Both apps running: Creaves on `:3000`, Console on `:3001`, MySQL up.
2. Console admin account exists (`buffalo task db:seed`, `admin`).
3. Console: an API key scoped to the Creaves instance id
   (*Webhook API Keys → Generate*, instance id = Creaves `config.instance_id`).
4. Creaves: *Config → edit* — webhook enabled, console URL
   `http://127.0.0.1:3001/webhook/events`, the API key, and the instance id.
5. Admin sessions for the curl snippets below (`creaves_…` = raw API key).

```bash
# console admin session
curl -s -c con.jar http://localhost:3001/auth/new          # take CSRF token
curl -s -b con.jar -c con.jar -X POST http://localhost:3001/auth \
  -d "login=admin&password=admin123&authenticity_token=$TOK"

# creaves admin session (creaves form uses capitalised Login/Password)
curl -s -c cre.jar http://localhost:3000/auth/new          # take CSRF token
curl -s -b cre.jar -c cre.jar -X POST http://localhost:3000/auth \
  -d "Login=admin&Password=admin&authenticity_token=$TOK"
```

## Scenario 1 — resync is idempotent (no cleanup)

1. Creaves UI: *Webhook resync → Start resync* (leave **Force full
   rebuild** unchecked).
2. Poll `GET /webhook_resync/status.json` until `status` is `none`.
3. Expected (`resync_runs`): `events_created=0`,
   `events_skipped_unchanged=<animal count>`, `errors` empty.
4. Console counts unchanged (dedup by deterministic event id):
   ```bash
   mysql … consolidation -e "SELECT COUNT(*) FROM event_streams WHERE instance_id='<id>'"
   ```

Validated live 2026-09-02 (instance `BigMac.local`, 10046 animals):
`completed, animals_processed=10046, events_created=0,
events_skipped_unchanged=10046, errors=""` — console rows identical.

## Scenario 2 — cleanup, then forced rebuild

1. Console UI: *Instances → Delete* on the instance. The dialog lists the
   animals/events/keys about to be deleted; type the instance_id to confirm.
   `POST …/cleanup` answers `303 → /instances`; wrong/missing confirmation
   answers `422`.
2. The purge also deletes the instance's console API keys — generate a new
   one (*Webhook API Keys*; the raw key is shown once) and paste it into the
   Creaves webhook config.
3. Creaves UI: *Webhook resync → Start resync* with **Force full rebuild**
   checked.
4. Expected: every state event re-queued and delivered; the console
   auto-registers the instance and rebuilds animals with identical final
   state; a *normal* resync afterwards is again a no-op (Scenario 1).

Validated live 2026-09-02 (instance `BigMac.local`):

| Step | Evidence |
|---|---|
| Dialog counts | index rendered `data-instance-id="BigMac.local" data-animals="10028" data-events="10047" data-keys="1"` |
| Cleanup | `POST /instances/BigMac.local/cleanup` → 303; MySQL: events 0, animals 0, registry 0, keys 0 |
| Force resync | `POST /webhook_resync/start` with `force=true` → 303; run `completed` |
| Rebuild | console: `event_streams` 10046 rows for `BigMac.local`, `consolidated_animals` 10027 (unique animals), `creaves_instances` row re-created |
| Idempotency | subsequent normal resync: created 0 / skipped 10046, console unchanged |

## Notes

- Force mode exists because the resync dedup is Creaves-local; after a
  console-side purge only a forced run re-sends anything. Re-delivered
  events keep their deterministic UUIDs, so the console rebuild never
  duplicates data.
- First delivery of a big instance is rate/batch limited by the Creaves
  webhook config (`WebhookBatchSize`, `WebhookMaxPerMin`); the validation
  above used batch 100 / 60000 per minute to keep the run short.
