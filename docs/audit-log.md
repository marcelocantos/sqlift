# Audit Log

Chronological record of audits, releases, documentation passes, and other
maintenance activities. Append-only — newest entries at the bottom.

## 2026-02-22 — open-sourcing (reconstructed)

- **Commit**: `740eb58`
- **Outcome**: Initial open-source release of sqlift — declarative SQLite schema migration library. Added LICENSE (Apache 2.0), CLAUDE.md, and agents-guide.md for agentic coding tools. Also added JSON serialization for MigrationPlan.

## 2026-02-22 — release/v0.1.0 (reconstructed)

- **Commit**: `740eb58`
- **Outcome**: Tagged v0.1.0 at initial release commit.

## 2026-02-22 — release/v0.2.0 (reconstructed)

- **Commit**: `f84f17a`
- **Outcome**: Tagged v0.2.0 after adding JSON serialization for MigrationPlan.

## 2026-02-23 — release/v0.3.0 (reconstructed)

- **Commit**: `43bcba9`
- **Outcome**: Tagged v0.3.0 after replacing Makefile with mkfile and vendoring doctest.

## 2026-02-23 — release/v0.4.0 (reconstructed)

- **Commit**: `f88e1d3`
- **Outcome**: Tagged v0.4.0 after adding breaking change detection, richer FK errors, and int64 bindings.

## 2026-02-25 — /audit (reconstructed)

- **Commit**: `5dbda8f`
- **Outcome**: Comprehensive pre-v0.6.0 audit. 1 Critical, 2 High, 3 Medium, 2 Low, 17 Info findings. All 8 actionable items resolved in follow-up commits same day: added NOTICES for third-party attribution, updated docs/reference.md and docs/guide.md, switched Column::generated to GeneratedType enum, moved sha256() to file-local scope, updated README dependency note, updated nlohmann/json from v3.11.3 to v3.12.0, and added .gitignore entries.
- **Deferred**:
  - extract() and parse_create_table_body() noted as longest functions (~140-160 lines), worth watching for future maintainability

## 2026-02-25 — documentation-pass (reconstructed)

- **Commit**: `ca750a6`
- **Outcome**: Applied audit findings to docs: updated reference.md and guide.md, hid sha256, fixed GeneratedType enum documentation.

## 2026-02-25 — release/v0.6.0 (reconstructed)

- **Commit**: `8ebfb2f`
- **Outcome**: Tagged v0.6.0 (note: v0.5.0 not found in tags — likely skipped). Bumped version macros after audit fixes and nlohmann/json dependency update landed.

## 2026-02-28 — /open-source v0.7.0

- **Commit**: `426d160`
- **Outcome**: Full open-source readiness pass. Phase 1 (Audit): 22 findings (2H, 7M, 11L, 3I) — report at `docs/audit-2026-02-28.md`. Phase 2 (Fixes): all High and Medium findings resolved — FK enforcement restoration on error, parser string-literal awareness, GeneratedType validation, hash consistency, vendor LICENSEs. Phase 3 (Docs): updated STABILITY.md with `pk_constraint_name` and `constraint_name` fields; all other docs already current. Phase 4 (Publish): pushed to origin, disabled unused wiki. Phase 5 (Release): added CI workflow (`.github/workflows/ci.yml` — tests + sanitizer on ubuntu + macOS), bumped to v0.7.0, created GitHub release. CI green on first run.
- **Deferred**:
  - 11 Low + 3 Info audit findings (see `docs/audit-2026-02-28.md`)
  - Redundant index detection (TODO)
  - 1.0 settling threshold: 0/3 consecutive minor releases without breaking changes

## 2026-03-01 — /release v0.8.0

- **Commit**: `3ec729f`
- **Outcome**: Released v0.8.0. Go port of full library (`go/sqlift` package, 127 tests), cross-language SHA-256 hash verification, Go CI job (test + vet on ubuntu + macOS), source files moved to `dist/`, README/STABILITY.md/agent guide updated for both C++ and Go, repo description updated.
- **Notes**:
  - C++ tests: 109 (was 108), 340 assertions
  - Go tests: 127
  - 1.0 settling threshold: 1/3 consecutive minor releases without breaking changes

## 2026-03-01 — /release v0.9.0

- **Commit**: `c268876`
- **Outcome**: Released v0.9.0. Documentation overhaul: new getting-started tutorial (`docs/getting-started.md`), Go API reference (`docs/reference-go.md`), guide rewrite with declarative-vs-numbered pitch and bilingual C++/Go examples, C++ reference cross-references, README doc links updated. STABILITY.md: added `migration_version` to C++ catalogue, resolved Go docs gap.
- **Notes**:
  - C++ tests: 109, 340 assertions
  - Go tests: 127
  - 1.0 settling threshold: 2/4 consecutive minor releases without breaking changes (surface expanded to ~62 items, N increased from 3 to 4)

## 2026-03-01 — /release v0.10.0

- **Commit**: `82c4c72`
- **Outcome**: Released v0.10.0. Redundant index detection: `detect_redundant_indexes()` / `DetectRedundantIndexes()` in both C++ and Go, integrated into `diff()` / `Diff()` with warnings on `MigrationPlan`. Detects PK-duplicate, prefix-duplicate, and exact-duplicate indexes. New types: `WarningType`, `Warning`. STABILITY.md updated with new surface items; "Redundant index detection" removed from out-of-scope list.
- **Notes**:
  - C++ tests: 122, 368 assertions
  - Go tests: 140
  - 1.0 settling threshold: 4/4 — threshold reached. Remaining gap: Go `Example*` test functions
