# Learning v2 implementation validation

This branch implements P1–P8 on top of the P0 design. Results below distinguish
local checks, CI with PostgreSQL/Compose, and integrations requiring real accounts.

## P1 — model discovery and task routing

- Added bounded authenticated GET discovery, same-origin pagination, catalog
  snapshots, metadata provenance, explicit selection and bulk state operations.
- Connected the shared scope selector to the real identity-group and task screens;
  saving a provider opens discovery. Manual model entry and TTS remain available.
- Added explicit ordered fallbacks, finite attempts/time/input/output budgets,
  per-call usage records and transient concurrency protection. Legacy task pools
  retain fallback disabled until the administrator enables it.
- Local: Go 1.26 `go test -race -count=1 ./...` passed (database-dependent cases
  skipped without TEST_DATABASE_URL); Web 17 tests and production build passed.
- New tests cover metadata absence, authenticated discovery, unsafe pagination,
  bounded attempts/concurrency and a PostgreSQL-backed revoked fallback scenario.
- Real supplier credentials and throughput/voice quality were not tested. CI must
  execute database integration and migration checks before review readiness.

## P2 — administration and account safety

- Ordinary model/resource/vocabulary/registration operations now use the current
  administrator session, CSRF and fresh role check. Account mutations retain
  password reauthentication; existing password JSON fields remain accepted for
  old clients without being required for routine changes.
- Shared administrator navigation and deep-link handling preserve existing URLs.
- Bootstrap now requires initial password change. Server-only interactive reset
  hides password input, confirms the target, revokes sessions, requires change and
  audits recovery; disabled accounts remain disabled.
- Added group edit revisions and regression tests for routine management without
  a password, CSRF/learner rejection, recovery and initial password gating.
- Local Go race/unit and Next production build passed. Database regressions are
  included in CI; no production account was modified.

P1 CI with PostgreSQL 17, race tests, browser mocks and complete Compose passed: https://github.com/basumobai/learning/actions/runs/38948876823 .
