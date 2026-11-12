# Creaves Console Documentation

## Current documentation

| Document | Scope |
| --- | --- |
| [../README.md](../README.md) | Features, architecture, setup, API operations, CLI, testing, deployment, and troubleshooting |
| [../AGENTS.md](../AGENTS.md) | Concise engineering, security, localization, and validation rules |
| [specs/SYNC_SPEC.md](specs/SYNC_SPEC.md) | Canonical webhook v2 envelope, events, responses, checksums, and compatibility rules |
| [e2e-cleanup-resync-walkthrough.md](e2e-cleanup-resync-walkthrough.md) | Current instance-cleanup/full-resync recovery procedure |
| [../../creaves/docs/SETUP_AND_TESTING.md](../../creaves/docs/SETUP_AND_TESTING.md) | Combined local setup and cross-application validation |
| [../../creaves/e2e/RESYNC_RUNBOOK.md](../../creaves/e2e/RESYNC_RUNBOOK.md) | Producer-side resync procedure |

## Historical material

[archive/](archive/) preserves the removed pull-model design, research, implementation/subtask plans, and completed fix reports. These files are **not current setup or design authority**. In particular, direct source-database connections and `source_instances` are obsolete; webhook push is the current ingestion model.

## Maintenance rules

- Keep active pages limited to current behavior, contracts, design, operations, and recovery.
- Update the specification and both applications together for contract changes.
- Avoid duplicating the full payload schema outside `specs/SYNC_SPEC.md`.
- Move completed research, plans, and dated reports to `docs/archive/` and repair links.
- Keep historical evidence out of agent onboarding and everyday operational documentation.
