# TODO

## Audit follow-ups

- [x] Address remaining Low findings from [audit-2026-02-28.md](audit-2026-02-28.md) (10 Low)
- [x] Address remaining Info findings from [audit-2026-02-28.md](audit-2026-02-28.md) (3 Info)

## Features

- [x] Redundant index detection — warn when desired schema has prefix-duplicate or PK-duplicate indexes

## API

- [ ] **C API should accept `sqlite3*` directly** — `sqlift_extract`,
  `sqlift_apply`, and other functions that operate on a database currently
  require a `sqlift_db*` opaque handle. Callers like sqlpipe that already
  have a `sqlite3*` must wrap it via a shim (`sqlift_db_wrap`) that opens
  a dummy `:memory:` db, closes it, and memcpy's the borrowed handle in.
  The C API should accept `sqlite3*` directly for these operations.
