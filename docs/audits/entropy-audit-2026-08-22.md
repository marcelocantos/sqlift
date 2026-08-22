# Entropy audit — sqlift — 2026-08-22

## Executive summary

- **Snapshot:** `/Users/marcelo/work/github.com/marcelocantos/sqlift`
- **Branch:** `master` tracking `origin/master`
- **HEAD:** `8c5e15760fe350e96532a90d3aa3c68bf0c7b6d5` — *Richer apply error messages for on-device diagnosis (#21)* (2026-07-14)
- **Tag:** `v0.18.0`
- **Initial dirty state (before this audit wrote anything):** `?? docs/audit/` (untracked, user-owned; not read as a source of truth)
- **Date:** 2026-08-22
- **Hygiene:** not declared (`hygiene.yaml` absent)
- **Languages analysed:** C public ABI, C++23 implementation, Go/cgo wrapper, SQLite DDL, GitHub Actions shell

**Headline mechanism:** a deliberately two-file C library with a Go/cgo façade is internally coherent, but three competing catalogues (code, `STABILITY.md`, agent docs) describe different apply/diff contracts, and `apply()` is atomic only inside a single rebuild savepoint — not across a multi-operation plan.

**Highest-consequence findings:**

- **ENT-001 (P1):** `sqlift_apply` commits each non-rebuild statement under SQLite autocommit. After a stored `_sqlift_state` hash exists, a later multi-op plan that fails mid-way leaves the live schema ahead of the stored hash and the next `apply` raises permanent `SQLIFT_DRIFT_ERROR`.
- **ENT-002 (P1):** `CLAUDE.md`, `STABILITY.md`, `docs/reference.md`, `docs/guide.md`, and `dist/sqlift-agents-guide.md` still teach `sqlift_diff` / `Diff` as the breaking-change gate and omit `CREATE VIRTUAL TABLE`, while the shipped code tags `data_dependent` at diff time and gates at apply time, and `tests/test_virtual.cpp` is live.

**Unverified residue (owner judgment only):** whether whole-plan atomicity should wrap apply in one transaction after lifting `PRAGMA foreign_keys` to the apply envelope; whether Go `Schema.Hash()` should remain a native reimplementation; whether the C ABI should take `sqlite3*` (documented in `docs/TODO.md`, not implemented). Mechanical checks that were skipped are listed under oracle coverage, not here.

## Scope and exclusions

**In scope:** `dist/sqlift.{h,cpp}`, `tests/`, `go/sqlift/*.go`, `cvfile`, `.github/workflows/ci.yml`, public docs (`README.md`, `STABILITY.md`, `CLAUDE.md`, `docs/guide.md`, `docs/reference.md`, `docs/reference-go.md`, `dist/sqlift-agents-guide.md`), `docs/TODO.md`, `LICENSE`, `NOTICES`, `.gitignore`.

**Named exclusions (not mixed into production conclusions):**

| Tree | Role |
|---|---|
| `vendor/include/doctest/` | test-only header (MIT, NOTICES) |
| `vendor/include/nlohmann/json.hpp` | vendored runtime JSON (MIT, NOTICES); Go copy under `go/sqlift/include/nlohmann/` is a CI-synced bundle, not a second implementation |
| `go/sqlift/sqlift.cpp`, `go/sqlift/sqlift.h` | byte copies of `dist/` (`cv bundle`; CI `git diff`) |
| `build/` | local compile artefacts (gitignored) |
| `.cv/` | cv cache produced by this audit’s `cv test` / `cv sanitize`; **not gitignored** (see ENT-007) |
| `docs/audit/` | untracked user-owned tree; not used as evidence |
| `docs/audit-2026-02-24.md`, `docs/audit-2026-02-28.md` | prior audits in a different format; used only as historical context |

No Windows, Linux-cross, or live consumer (sqlpipe/mnemo) path was executed.

## Commands run

| Command | Version | Exit | Shipped path? | Notes |
|---|---|---|---|---|
| `git rev-parse HEAD`; `git status --porcelain=v1 -b` | git | 0 | n/a | Initial snapshot recorded before any write |
| `diff -q dist/sqlift.cpp go/sqlift/sqlift.cpp` (and `.h`, `json.hpp`) | diff | 0 (no output) | auxiliary | Bundled copies match `dist/` / `vendor/` |
| `cv test` | cv v0.10.0; Apple clang 21.0.0 | 0 | **yes** (`cvfile` `!test`) | doctest 2.4.12: **152 cases, 449 assertions, 0 failed** |
| `cv sanitize` | same + ASan/UBSan | 0 | **yes** (`!sanitize`; CI job `test`) | **152/152** |
| `cd go/sqlift && GOWORK=off CGO_LDFLAGS=-lsqlite3 go test -count=1 .` | go 1.26.4 | 0 | **yes** (CI `test-go`) | `ok`; 30 `--- PASS` lines. `GOWORK=off` required because a parent `go.work` does not list this module |
| `go vet ./...` (same cwd/env) | go 1.26.4 | 0 | **yes** (CI) | |
| `~/.claude/skills/hygiene/hygiene_check.py` | hygiene skill | 1 | auxiliary | `FileNotFoundError: .../hygiene.yaml` |
| `sqlite3 --version` | 3.51.0 | 0 | auxiliary | Host SQLite, not a matrix pin |

**Limitations:** no clone detector, no coverage %, no Windows job, no `go test` race detector, no fuzzing, no secret scan. Metrics were not used as verdicts.

## Observed architecture

### Deployable units

1. **C library (source of truth).** Two-file dist: `dist/sqlift.h` (C ABI, JSON strings) + `dist/sqlift.cpp` (all C++ types and logic). Consumers copy the two files and link system SQLite3.
2. **Go module.** `github.com/marcelocantos/sqlift/go/sqlift` compiles the bundled `sqlift.cpp` via cgo. Does not `#cgo LDFLAGS: -lsqlite3` (consumer brings sqlite3).
3. **No CLI.** Library only.

### Internal topology (all inside `dist/sqlift.cpp`)

```
sqlift_parse  → parse()  → temp :memory: Database::exec(DDL) → extract()
sqlift_extract → extract(sqlite3*)  [sqlite_master + PRAGMAs + body parser]
sqlift_diff    → schema_from_json ×2 → diff() → to_json
sqlift_apply   → from_json → policy gates → extract+hash drift → exec ops → store hash
sqlift_schema_hash / detect_redundant_indexes → schema_from_json → Schema::hash / warnings
```

Internal C++ `extract` / `apply` already take `sqlite3*`. The C ABI wraps a `sqlift_db` that **owns** a `sqlift::Database` (`dist/sqlift.cpp:2772-2776`). There is no `sqlift_db_wrap`.

Dependency direction is acyclic: C wrapper → C++ engine → sqlite3 / nlohmann. Go wrapper → C ABI → JSON → Go domain types. Go `Schema.Hash()` is the one engine-shaped exception (ENT-003).

### Declared vs observed rules

| Rule | Status |
|---|---|
| C-only public API; C++ types stay in the `.cpp` | **enforced** by the header |
| `dist/` is SoT for the Go bundle | **enforced** by `cv bundle` + CI `git diff` |
| `sqlift_diff` is pure; never touches a DB | **enforced** |
| Breaking / data-dependent changes are an **apply-time** policy gate | **enforced in code** (`diff` sets `data_dependent`; `apply` throws). **Contradicted** by several docs (ENT-002) |
| Rebuild is SQLite’s 12-step sequence with FK restore on failure | **enforced**; tested in `tests/test_apply.cpp:324-372` |
| No rename detection | **enforced** by design |
| `raw_sql` excluded from equality | **enforced** in `Table::operator==` / Go `Table.Equal` |
| Virtual tables: drop+recreate, shadow-table filter | **enforced** in code + `tests/test_virtual.cpp`. **Absent** from user/agent docs (ENT-002) |
| C ABI accepts caller `sqlite3*` | **documented intent** in `docs/TODO.md:14-19`; **not implemented** (ENT-006) |

## Dimension vector

No scalar. No prior `docs/audits/entropy-audit-*.md` baseline.

| Dimension | State | Evidence summary | Change from baseline |
|---|---|---|---|
| Architecture topology | concern | Clear C ABI + Go façade; internal `sqlite3*` not exported; apply envelope is per-op autocommit except rebuild savepoints | n/a |
| Redundancy / sources of truth | concern | Bundle copies are ratcheted; hash serialisation, plan JSON, and the stability/agent catalogues are not | n/a |
| Change amplification | concern | Feature work (virtual tables, allow bits) landed in code/tests but not in STABILITY/CLAUDE/agent guide; Go must re-port hash/JSON/OpType | n/a |
| Local code quality | healthy | RAII `Database`/`Statement`, `quote_id`, linear 2962-line TU is the declared two-file shape; sanitizers green | n/a |
| Correctness / verification | concern | 152 C++ + Go package tests + ASan/UBSan green; no oracle for multi-op apply failure after a stored hash; warning JSON unescaped | n/a |
| Security / dependencies | healthy | Apache-2.0 + NOTICES; identifiers quoted; parse executes caller DDL by design; no secrets; cv installer unpinned (ENT-007) | n/a |
| Build / release / operations | concern | `cv test`/`sanitize` and dual OS CI exist; `.gitignore` still has `.mk/` not `.cv/`; CI fetches **latest** cv; README still leads with `mk` | n/a |
| Documentation / governance | concern | `STABILITY.md` is the 1.0 catalogue and is stale on Go `ApplyOptions`, `RebuildError`, virtual tables; `CLAUDE.md` still says 7 files / 125 tests and diff-time breaking errors | n/a |

## Findings

### ENT-001: Multi-op `apply` is not atomic; failure after a stored hash wedges on drift

- **Priority:** P1
- **Dimensions:** Architecture topology; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - `apply()` loops operations and statements with no `BEGIN` (`dist/sqlift.cpp:2199-2234`). Hash is written only after the whole loop (`:2253-2255`).
  - Rebuild SQL itself starts with `PRAGMA foreign_keys=OFF` then `SAVEPOINT sqlift_rebuild` (`:1354-1358`). Failure handling rolls back **that** savepoint only (`:2235-2251`) and comments that `PRAGMA foreign_keys` cannot be changed inside a transaction (`:2237`).
  - SQLite default autocommit therefore commits `CREATE TABLE` / `CREATE INDEX` / `DROP …` as soon as each statement finishes.
  - Recovery tests cover **rebuild** FK-check failure (`tests/test_apply.cpp:324-355`: table contents preserved, no leftover `*_sqlift_new*`, FK pragma restored). There is **no** test where op 1 (non-rebuild) commits and op 2 fails after `_sqlift_state.schema_hash` already exists.
  - Drift is then detected against the stored hash (`dist/sqlift.cpp:2179-2189`; `tests/test_apply.cpp:445-460` only covers *out-of-band* `ALTER TABLE`).
- **Mechanism:** First apply stores a hash. A later plan with ≥2 ops (e.g. create table + create index, or drop+create virtual table plus other work) can leave the live schema mutated if a later statement fails. The stored hash still describes the pre-plan schema. The next `sqlift_apply` throws `SQLIFT_DRIFT_ERROR` with no in-library repair path. First-ever apply (empty stored hash) skips the drift check and can recover via re-diff — the wedge is specifically post-v1.
- **Blast radius:** Any consumer doing incremental migrations (sqlpipe on-device is the stated apply-error audience in `tests/test_apply.cpp:70-71`). Rebuild-only plans remain savepoint-atomic.
- **Counterevidence checked:** Single-rebuild failure is recovered (test above). Empty plans return before touching the DB (`:2141`). Policy gates run before any SQL (`:2149-2177`). This is not a successful-path corruption bug.
- **Smallest coherent remediation:** Lift `PRAGMA foreign_keys` off/on to the apply envelope (already recorded at `:2192-2197`). Strip those pragmas from `rebuild_table_sql`. Wrap the remaining statements in one `BEGIN`/`COMMIT` (or a single savepoint). On failure, roll back the envelope, restore FK state, do not write a hash. Keep per-rebuild savepoints only if they still buy anything after the envelope exists.
- **Verification:** A doctest that (1) applies v1, (2) applies a two-op v2 plan whose second statement is forced to fail, (3) asserts the v1 schema is intact **and** a subsequent apply of v2 succeeds without `SQLIFT_DRIFT_ERROR`.
- **Ratchet candidate:** that test on the shipped `cv test` / CI `test` job.

### ENT-002: Stability and agent catalogues contradict the shipped apply/diff contract and omit virtual tables

- **Priority:** P1
- **Dimensions:** Documentation / governance; Change amplification; Redundancy / sources of truth
- **Status:** observed fact
- **Evidence:**
  - Code: `diff()` explicitly does **not** throw on data-dependent changes (`dist/sqlift.cpp:1680-1682`, `:1789-1798`); `apply()` throws `BreakingChangeError` when `data_dependent && !allow_data_dependent` (`:2164-2169`). `sqlift_diff` C wrapper always serialises the plan (`:2830-2838`).
  - Tests: `tests/test_roundtrip.cpp:117-136` and `tests/test_apply.cpp:166-178` assert `SQLIFT_BREAKING_CHANGE_ERROR` from **apply**, not diff. 15 `TEST_CASE`s in `tests/test_virtual.cpp`; Go `virtual_test.go` covers the same surface.
  - Contradictions:
    - `CLAUDE.md:35` — “`sqlift_diff()` returns `SQLIFT_BREAKING_CHANGE_ERROR` …”
    - `docs/reference.md:224-225` — `sqlift_diff` `err_type` includes `SQLIFT_BREAKING_CHANGE_ERROR`
    - `docs/guide.md:502` — “`diff()` raises `BreakingChangeError`”
    - `dist/sqlift-agents-guide.md:298` — “When `sqlift_diff()` returns `SQLIFT_BREAKING_CHANGE_ERROR`…”
    - `go/sqlift/diff.go:127-128` — `Diff` “returns a `*BreakingChangeError`”
    - `STABILITY.md:322-324` still shows `ApplyOptions{ AllowDestructive bool }` (the v0.13 shape). Shipped Go API is `Allow AllowFlags` (`go/sqlift/diff.go:117-122`). Error catalogue at `STABILITY.md:341-350` omits `*RebuildError` (exists in `go/sqlift/errors.go:37-42`).
    - Virtual tables: `OpType::CreateVirtualTable` / `DropVirtualTable` (`dist/sqlift.cpp:259-260`), schema JSON `virtual_tables` (`:2575-2588`). **Zero** hits for `virtual_tables` / `CreateVirtualTable` in `docs/*.md` or `STABILITY.md`. `docs/reference.md:590-592` and `dist/sqlift-agents-guide.md:166-168` list operation types **without** the virtual-table variants. `docs/reference-go.md:274-279` `Schema` struct omits `VirtualTables`.
    - `CLAUDE.md:54` still says “7 files, 125 tests”; this snapshot is **8** test TUs, **152** cases (`test_virtual.cpp` is the eighth). `CLAUDE.md:84-91` file list omits `test_virtual.cpp`.
  - `STABILITY.md:364-377` *does* describe the v0.14.0 move of `BreakingChangeError` to apply time — so the same file contains both the corrected narrative and the stale Go snippets.
- **Mechanism:** Agents and 1.0-settling reviews read `CLAUDE.md` / `STABILITY.md` / the agent guide. They will implement the wrong gate (expect `Diff` to fail) and will not discover virtual-table ops. The next schema feature must be edited in code, C tests, Go tests, C reference, Go reference, guide, agent guide, STABILITY, and CLAUDE — and several of those already lagged v0.15–v0.18.
- **Blast radius:** every FFI consumer following the agent guide; 1.0 eligibility (`STABILITY.md` settling counter) is judging the wrong surface.
- **Counterevidence checked:** `docs/reference.md` apply section (`:285-293`) and `docs/reference-go.md` `Apply` (`:175-183`) correctly describe apply-time gates. `docs/guide.md:270-274` is also correct. The defect is **split-brain docs**, not total absence of the new contract.
- **Smallest coherent remediation:** Make `STABILITY.md` a generated or mechanically diffed snapshot of `dist/sqlift.h` + `go/sqlift/*.go` exported types. Patch the five false “diff throws BreakingChange” sentences in one commit with the virtual-table JSON/op-type additions. Update `CLAUDE.md` test inventory from `cv test` output.
- **Verification:** a test or CI step that greps the agent guide / CLAUDE / STABILITY for `sqlift_diff.*BREAKING_CHANGE` (must be empty) and that `CreateVirtualTable` appears in `docs/reference.md` and `STABILITY.md`.
- **Ratchet candidate:** file-content check in CI, or a small `tests/` assertion that the documented op-type list in a frozen snippet matches `to_string(OpType)`.

### ENT-003: Schema hash has two independent implementations

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification
- **Status:** observed fact
- **Evidence:**
  - C++: `Schema::hash()` serialises then calls a file-local `sha256()` (`dist/sqlift.cpp:529-591`, `:474`). Used by `apply` for `_sqlift_state` (`:2183`, `:2255`) and by `sqlift_schema_hash` (`:2887-2892`).
  - Go: `Schema.Hash()` re-serialises the same format and calls `crypto/sha256` (`go/sqlift/schema.go:238-378`). Comment at `:239-240` and `:361-362` requires byte-for-byte match with C++.
  - Oracles are **frozen hex strings**, not a live C-vs-Go call: `tests/test_roundtrip.cpp:90-114` and `go/sqlift/schema_test.go:219-245` share `e712ade6…`; vtable snapshots `tests/test_virtual.cpp:231-253` and `go/sqlift/virtual_test.go:181-198` share `0653de74…`.
  - Apply on both languages stores the **C** hash (Go `Apply` calls `C.sqlift_apply`). Go `Hash()` is a public API (`STABILITY.md:261-262`) that can disagree with the stored hash if the serialisers drift in ways the two fixtures do not cover.
- **Mechanism:** Adding a hashed field (virtual tables already did) requires editing two serialisers, two SHA implementations, and two snapshot constants. A missed Go edit would not break `Apply`; it would break consumers comparing `Schema.Hash()` to `_sqlift_state`.
- **Blast radius:** cross-language drift detection for any caller that uses Go `Hash()` rather than `sqlift_schema_hash`.
- **Counterevidence checked:** snapshots exist and currently match (`cv test` and `go test` green). Empty vs missing `virtual_tables` is hashed identically on both sides (C `:583-585`; Go `:359-363`). This duplication is **deliberate** for a pure-Go method that does not round-trip JSON.
- **Smallest coherent remediation:** implement `Schema.Hash()` via `sqlift_schema_hash` on marshalled JSON (one serialiser). Keep the frozen fixtures as a regression net on the C function.
- **Verification:** for every `Parse` fixture, `s.Hash() == C.sqlift_schema_hash(json)`.
- **Ratchet candidate:** that comparison inside `TestCrossLanguageHash` over a table of DDLs, not one string.

### ENT-004: Plan JSON and OpType maps are ported twice

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification
- **Status:** observed fact
- **Evidence:**
  - C++ `to_json` / `from_json` / `sqlPrefix` (`dist/sqlift.cpp:2320-2477`).
  - Go `ToJSON` / `FromJSON` / `sqlPrefixForOpType` (`go/sqlift/json.go:77-354`), with comments “Port of C++ … lines 1595-1613” (line numbers already stale vs current 2320).
  - `OpType.String` / `ParseOpType` (`go/sqlift/json.go:13-75`) vs `to_string` / `op_type_from_string` (`dist/sqlift.cpp:2305-2317`).
  - Go `Diff` unmarshalls C JSON with `FromJSON` (`go/sqlift/diff.go:152`); Go `Apply` re-encodes with `ToJSON` then C `from_json` (`go/sqlift/apply.go:28-40`). Two validators must accept the same wire.
- **Mechanism:** A new op type (virtual tables) or a new plan field (`requires_rebuild`, `loosens_only`, `data_dependent`) must be added in both languages plus both test suites. Prefix-validation bugs can be fixed on one side only.
- **Blast radius:** plan files exchanged between C and Go; `FromJSON` of hand-edited JSON.
- **Counterevidence checked:** `tests/test_json.cpp` and `go/sqlift/json_test.go` both cover round-trip and prefix mismatch. Dual validation is partly **necessary** because Go exposes typed `MigrationPlan` rather than raw JSON.
- **Smallest coherent remediation:** keep Go as a thin JSON view of C (`sqlift_diff` / `sqlift_apply` already speak JSON). Generate `OpType` string maps from one table, or drop Go prefix validation and trust C `from_json` on apply.
- **Verification:** fuzz / corpus of plan JSON through Go `FromJSON` then C `sqlift_apply` dry policy (empty DB) and the reverse.
- **Ratchet candidate:** one shared JSON corpus in `tests/` consumed by both suites.

### ENT-005: `sqlift_detect_redundant_indexes` concatenates unescaped JSON

- **Priority:** P2
- **Dimensions:** Local code quality; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - `warnings_to_json` concatenates `w.message`, `index_name`, `covered_by`, `table_name` with no escape (`dist/sqlift.cpp:2751-2766`). Comment at `:2757` claims “no escaping issues in practice” and `:2750` still says it is reused by `sqlift_diff` — it is **not**; only `sqlift_detect_redundant_indexes` calls it (`:2880`). Plan warnings go through nlohmann (`:2340-2351`).
  - Go `DetectRedundantIndexes` unmarshalls that JSON and **returns nil** on parse error (`go/sqlift/warning.go:59-61`), hiding the failure.
  - SQLite identifiers may contain `"`; `quote_id` already doubles quotes for SQL (`dist/sqlift.cpp:614-618`) but the warning JSON path does not.
- **Mechanism:** A redundant index whose name or message contains `"` or `\` yields invalid JSON. C callers get a syntactically broken string with `SQLIFT_OK`. Go callers get an empty warning list.
- **Blast radius:** `sqlift_detect_redundant_indexes` and Go `DetectRedundantIndexes` only. Plan JSON from `sqlift_diff` is safe (nlohmann).
- **Counterevidence checked:** no test with quoted identifiers in index names. Plan-path warnings are escaped.
- **Smallest coherent remediation:** delete `warnings_to_json`; serialise warnings with nlohmann (already included at `:332`). Make Go return an error on JSON failure.
- **Verification:** schema with `CREATE INDEX "a\"b" …` that is prefix-redundant; C result must parse as JSON; Go must return a warning, not nil.
- **Ratchet candidate:** that case in `tests/test_diff.cpp` “standalone detect_redundant_indexes”.

### ENT-006: C ABI cannot borrow a caller `sqlite3*`

- **Priority:** P2
- **Dimensions:** Architecture topology
- **Status:** observed fact
- **Evidence:**
  - Internal functions: `extract(sqlite3*)`, `apply(sqlite3*, …)` (`dist/sqlift.cpp:232`, `:308`, `:956`, `:2140`).
  - C ABI: `sqlift_extract` / `sqlift_apply` require `sqlift_db*` that constructs and owns `sqlift::Database` (`:2772-2776`, `:2819-2823`, `:2845-2856`).
  - `docs/TODO.md:14-19` describes a `sqlift_db_wrap` shim (dummy `:memory:` open, close, memcpy handle) that **does not exist in this repository** (grep hits only that TODO).
- **Mechanism:** In-process consumers that already own `sqlite3*` (sqlpipe is named) cannot call extract/apply on that connection without a second `sqlift_db_open` or an out-of-tree hack. Drift hash and migrations then run on a **different** connection than the application’s, which is the wrong `_sqlift_state` and the wrong FK pragma.
- **Blast radius:** every C/C++ embedder with an existing connection; FFI that is not cgo’s `sqlift.Open`.
- **Counterevidence checked:** Go does not need this (it uses `sqlift.Open`). Tests always go through `sqlift_db_open`. Ownership-hiding is otherwise a healthy ABI choice.
- **Smallest coherent remediation:** `sqlift_db* sqlift_db_from_handle(sqlite3*, int borrowed)` that does not close a borrowed handle in `sqlift_db_close`, **or** change extract/apply to take `sqlite3*` as the TODO states. Do not memcpy over a closed handle.
- **Verification:** test that extract/apply on a borrowed handle see tables created on the original `sqlite3*` and write `_sqlift_state` there; close must not `sqlite3_close` the borrowed pointer.
- **Ratchet candidate:** that C API test; then retire the TODO bullet.

### ENT-007: mk→cv migration left unpinned CI, stale `mk` docs, and an unignored `.cv/` cache

- **Priority:** P2
- **Dimensions:** Build / release / operations; Documentation / governance
- **Status:** observed fact
- **Evidence:**
  - CI installs cv by querying the **latest** GitHub release (`.github/workflows/ci.yml:20-26`), twice (jobs `test` and `test-go`).
  - `.gitignore:13` still lists `.mk/`; `cvfile:83` cleans `.cv/` but git does not ignore it. After this audit’s `cv test`, `git status` showed `?? .cv/`.
  - `README.md:103` still leads with `mk` as the build command; `go/sqlift/cgo.go:5` says `` `mk bundle` ``; `go/sqlift/doc.go:13-15` says the package “requires the static library (`build/libsqlift.a`) to be built before `go build`” — false since the cgo bundle (`go/sqlift/cgo.go:4-7`, `cvfile:60-80`).
- **Mechanism:** A broken or incompatible `cv` release can turn sqlift CI red without a sqlift commit. Local `cv` runs dirty the tree with `.cv/`, which a later `git add .` could commit. Downstream Go docs that still require `libsqlift.a` send consumers to a path CI no longer uses.
- **Blast radius:** CI reproducibility; accidental commit of `.cv/state.json`; Go getting-started confusion.
- **Counterevidence checked:** `cvfile` itself is the real build graph; CI `test-go` runs `cv bundle` and fails on diff. `mk` is no longer on `PATH` in this environment.
- **Smallest coherent remediation:** pin `CV_VERSION` to a tag in both jobs; add `.cv/` to `.gitignore`; replace remaining `mk` / `libsqlift.a` sentences.
- **Verification:** CI workflow contains a `v0.x.y` literal for cv; `git status` after `cv test` is clean of `.cv/`.
- **Ratchet candidate:** `hygiene.yaml` `file:` / `ci_step:` evidence **when hygiene is onboarded** (do not add it in this audit).

### ENT-008: `schema_from_json` still unchecked-casts `GeneratedType`

- **Priority:** P3
- **Dimensions:** Local code quality; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - Live extract validates hidden ∈ {0,2,3} (`dist/sqlift.cpp:1034-1037`).
  - `schema_from_json` does `static_cast<GeneratedType>(jcol.value("generated", 0))` with no check (`:2621`). The 2026-02-28 audit treated the extract cast as Medium and marked it fixed; the JSON path was not included.
- **Mechanism:** Hand-built schema JSON (or a future SQLite value that survived a buggy extract skip) can carry `generated: 1` into hash/diff. Unlikely on the parse/extract shipped path.
- **Blast radius:** JSON-injected schemas only.
- **Counterevidence checked:** extract path is validated; tests round-trip 0/2/3 only (`tests/test_json.cpp:365`).
- **Smallest coherent remediation:** reuse the extract validation in `schema_from_json`; throw `JsonError`.
- **Verification:** `schema_from_json` with `"generated": 1` returns `SQLIFT_JSON_ERROR`.
- **Ratchet candidate:** one case in `tests/test_json.cpp`.

## Redundancy and competing-source-of-truth inventory

| Fact | Authorities | Drift risk | Disposition |
|---|---|---|---|
| C++ engine source | `dist/sqlift.cpp` + `dist/sqlift.h` | low | **single SoT** |
| Go-bundled C++ | `go/sqlift/sqlift.{h,cpp}`, `include/nlohmann/json.hpp` | low | **deliberate copy**, `cv bundle` + CI `git diff` |
| Schema hash serialisation | C++ `Schema::hash` vs Go `Schema.Hash` | high | ENT-003 |
| Plan JSON | C++ `from_json` vs Go `FromJSON` | medium | ENT-004 |
| Warning JSON | nlohmann in `to_json` vs `warnings_to_json` | medium | ENT-005 |
| Apply policy bits | C `#define SQLIFT_ALLOW_*` vs C++ `ApplyOptions` bools vs Go `AllowFlags` | low | translation at the C wrapper (`:2851-2855`); not competing |
| Public contract | `dist/sqlift.h` vs `STABILITY.md` vs `CLAUDE.md` vs agent guide vs `docs/reference*.md` | high | ENT-002 |
| Breaking-change gate | code (apply) vs several docs (diff) | high | ENT-002 |
| Virtual tables | code+tests vs user docs (absent) | high | ENT-002 |
| Build command | `cvfile` vs README `mk` vs `cgo.go` `mk bundle` | medium | ENT-007 |
| Test inventory | `cv test` (152) vs `CLAUDE.md` (125, 7 files) | low | ENT-002 |
| TODOs | `docs/TODO.md` vs global “no TODO files” rule | low | accepted in this repo’s `CLAUDE.md:78-80` |

## Healthy structure worth retaining

- **Two-file dist** with a C ABI and JSON interchange. C++ types, nlohmann, and SHA-256 never leak into `sqlift.h`. This is the product, not a packing accident.
- **Go bundle + CI sync.** `cvfile:60-80` and `.github/workflows/ci.yml:56-63` make the copy **owned**, not a silent fork. `diff -q` on this snapshot is clean.
- **Parse = exec DDL on `:memory:` then extract.** Parser correctness piggy-backs SQLite; `parse()` is 11 lines (`dist/sqlift.cpp:1198-1208`).
- **Policy bits are additive deny-by-default.** `sqlift_apply_options.allow` (`dist/sqlift.h:36-76`); zero-init is the strictest path.
- **Rebuild FK restore.** Prior 2026-02-28 T1 is closed: `apply` records `PRAGMA foreign_keys` (`:2192-2197`) and restores it on both failure and success (`:2244-2248`, `tests/test_apply.cpp:353-354`, `:357-372`).
- **Shadow-table filter fails loud.** Unknown vtable modules leak as normal tables (`dist/sqlift.cpp:909-911`) instead of corrupting the parent.
- **Identifier quoting** via `quote_id` on generated SQL (`:614+`).
- **In-memory tests, no fixtures on disk.** `cv test` / `go test` are the shipped oracles.
- **NOTICES + per-vendor LICENSE** for doctest 2.4.12 and nlohmann/json 3.12.0.
- **Cross-language hash snapshots** (even though the dual implementation is ENT-003) actually lock the wire format.

## Hygiene posture

`hygiene.yaml` is **absent**. Posture is **not declared**. This audit did not initialise one.

Validator invocation from repo root:

```
/Users/marcelo/.claude/skills/hygiene/hygiene_check.py
```

Exit 1. Full output:

```
Traceback (most recent call last):
  File "/Users/marcelo/.claude/skills/hygiene/hygiene_check.py", line 331, in <module>
    sys.exit(main())
  File "/Users/marcelo/.claude/skills/hygiene/hygiene_check.py", line 283, in main
    rep = check_repo(root, doc_path)
  File "/Users/marcelo/.claude/skills/hygiene/hygiene_check.py", line 237, in check_repo
    doc = yaml.safe_load(doc_path.read_text())
  File ".../pathlib/_local.py", line 546, in read_text
    ...
FileNotFoundError: [Errno 2] No such file or directory: '/Users/marcelo/work/github.com/marcelocantos/sqlift/hygiene.yaml'
```

Observed reality that a future `hygiene.yaml` could declare (not ratcheted here):

- `correctness`: `cv test`, `cv sanitize`, `go test`, `go vet` in CI on ubuntu+macos
- `build`: `cvfile`; cv **unpinned**
- `docs`: LICENSE, README, STABILITY — present, stale in places (ENT-002)
- `security` / `deps`: no secret scan, no Dependabot, no CODEOWNERS
- `governance`: Apache-2.0; no `hygiene.yaml`

Overlap: ENT-007’s pin/gitignore items are hygiene-shaped; ENT-001/003/005 are entropy mechanisms that become hygiene only after tests exist.

## Oracle coverage and residue

| Property | Decided by |
|---|---|
| Parse / extract / diff / apply happy paths, policy gates, drift (out-of-band ALTER), rebuild FK restore, JSON plan validation, redundant-index warnings | shipped `cv test` (152) + `go test` (30) |
| ASan/UBSan on the C++ suite | shipped `cv sanitize` |
| Go bundle byte-identity with `dist/` | CI `cv bundle && git diff` |
| Cross-language hash for **two** frozen schemas | doctest + `TestCrossLanguageHash` / `TestHashFormatSnapshot` |
| Multi-op apply failure after stored hash (ENT-001) | **nothing** |
| Warning JSON with special characters (ENT-005) | **nothing** |
| Docs/STABILITY vs `sqlift.h` (ENT-002) | **nothing** (manual) |
| Live `sqlite3*` borrow (ENT-006) | **nothing**; TODO only |
| Windows / non-cgo / sqlpipe-in-process | **nothing** (not claimed) |
| Go `Hash()` vs C `sqlift_schema_hash` on a broad corpus | **two snapshots only** |

Failed/skipped this run: hygiene validator (no yaml). `go test ./...` from repo root fails unless `GOWORK=off` and cwd is `go/sqlift` — environment, not a product defect.

**Owner residue (intent, not more grepping):**

1. Should apply be one transaction, given SQLite’s ban on changing `PRAGMA foreign_keys` inside a txn?
2. Should Go keep a native `Hash()` for schemas that never went through C JSON?
3. Should the C ABI take `sqlite3*` for 1.0, or is `sqlift_db` ownership the contract?
4. Is Windows in scope? README does not claim it.

## Remediation sequence

1. **Oracle first:** add the ENT-001 multi-op failure test (it will fail). Then lift FK pragmas to the apply envelope and wrap the rest in one transaction so that test passes. Keep the existing rebuild-recovery tests green.
2. **Converge catalogues:** one commit aligning `STABILITY.md`, `CLAUDE.md`, agent guide, `docs/reference.md`, `docs/guide.md:502`, and `Diff`’s godoc with apply-time gates **and** virtual tables. Stop treating `STABILITY.md` as a hand-copied API.
3. **Collapse hash SoT:** Go `Hash()` → `sqlift_schema_hash`; keep snapshots.
4. **Delete `warnings_to_json`**; do not return nil from `DetectRedundantIndexes` on bad JSON.
5. **C `sqlite3*` borrow** (ENT-006) if sqlpipe still needs it; otherwise rewrite the TODO as skipped-with-reason.
6. **Pin cv, ignore `.cv/`, drop `mk` / `libsqlift.a` sentences.**
7. After those tests exist, onboard `hygiene.yaml` (explicit request) and re-run this audit on the same finding IDs.

Do not rewrite `dist/sqlift.cpp` into many files to “fix” line count; the two-file dist is the architecture.
