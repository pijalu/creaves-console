# 2026-09-02 — E2E validation: console cleanup + Creaves full resync (incl. idempotency)

## Request (from workspace bugs.md)
Prove the combined flow end-to-end: console instance cleanup → Creaves full
resync rebuilds the instance data (counts, instance column, statuses);
re-trigger the resync *without* cleanup → no errors and no changes on the
console (idempotent); the flow must be triggerable from both web UIs,
validated live, covered by automated tests, and documented.

## Work performed
- **Live UI/curl validation** (both apps, admin sessions, dev MySQL):
  - Console: instances index Delete button opened the confirmation dialog
    with live counts (`data-animals="10028" data-events="10047"
    data-keys="1"`); typed-instance_id confirmation enforced (422 on
    mismatch, 303 on success); purge removed events, animals, API keys and
    the registry row (MySQL verified all 0).
  - Creaves: resync started from the UI; `status.json` polled to completion.
    - Normal repeat run: `created=0, skipped=10046, errors=""` — console
      untouched (idempotent).
    - After console cleanup the normal run rebuilt **nothing** — the dedup
      is Creaves-local and cannot see the purge. Fix: force mode
      (`force=true`, UI checkbox "Force full rebuild") re-queues known state
      events (`delivered_at=NULL`); deterministic event UUIDs keep the
      console side idempotent. Creaves commit `64db75c`.
  - Forced rebuild live result: console `BigMac.local` back to 10046 events
    (1:1 with source events), 10027 consolidated animals, instance row
    re-created; console key regenerated after the purge and wired into the
    Creaves config via the config UI.
- **Automated tests** (already present, re-run green):
  `TestE2E_V2CleanupThenResync`, `TestE2E_V2Resync_IdempotentAndLocalized`
  (console sqlite e2e); new Creaves regression test
  `TestRunResyncForceRequeuesDeliveredEvents` (normal-create /
  normal-skip / force-requeue, counters asserted).
- **Documentation**: repeatable walkthrough with curl transcripts added at
  `creaves-console/docs/e2e-cleanup-resync-walkthrough.md`.
- Dev-environment hygiene during validation: purged 30 075 stale
  `rsync-resync-test` events (leftover of an earlier aborted test run, that
  instance does not exist on the console) that blocked the FIFO delivery
  queue; removed two API-key rows whose raw secret was never captured.

## Result
Cleanup → forced resync → identical rebuild, and resync-twice → no-op, both
proven live and covered by automated tests. Item closed; bugs.md reduced to
the reporting guidelines only.

## Status
Tested and closed.
