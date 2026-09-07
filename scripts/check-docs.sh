#!/usr/bin/env bash
# Fail when the prose docs drift from the shipped sqlift contract.
#
# Each check below encodes something the code actually does today. When the
# code changes, update the check in the same commit as the docs.
set -uo pipefail

cd "$(dirname "$0")/.."

DOCS="CLAUDE.md STABILITY.md docs/guide.md docs/reference.md docs/reference-go.md dist/sqlift-agents-guide.md"
fail=0

report() {
  printf 'check-docs: FAIL: %s\n' "$1" >&2
  fail=1
}

# 1. The data-dependent gate lives in apply(), not diff() — see dist/sqlift.cpp
#    "We do NOT throw here — the policy gate belongs in apply()".
hits=$(grep -nE '(sqlift_)?diff\(\)[^.]*(returns|raises|rejects)[^.]*(SQLIFT_BREAKING_CHANGE_ERROR|BreakingChangeError)' $DOCS)
if [ -n "$hits" ]; then
  report "docs attribute the breaking-change error to diff(); apply() raises it"
  printf '%s\n' "$hits" >&2
fi

# 2. Virtual-table operations are shipped (OpType::CreateVirtualTable) and must
#    appear in the operation-type catalogues.
for f in docs/reference.md STABILITY.md dist/sqlift-agents-guide.md; do
  grep -q 'CreateVirtualTable' "$f" || report "$f does not mention CreateVirtualTable"
done

# 3. The Go Schema struct carries VirtualTables (go/sqlift/schema.go).
grep -q 'VirtualTables' docs/reference-go.md || report "docs/reference-go.md omits Schema.VirtualTables"

# 4. ApplyOptions is a bitmask (go/sqlift/diff.go: type AllowFlags uint32).
grep -q 'AllowDestructive bool' STABILITY.md && report "STABILITY.md documents the removed 'AllowDestructive bool' shape"

# 5. RebuildError is a public Go error type (go/sqlift/errors.go).
grep -q 'RebuildError' STABILITY.md || report "STABILITY.md error catalogue omits *RebuildError"

# 6. The test-suite inventory in CLAUDE.md must match tests/ on disk.
suites=$(ls tests/*.cpp | wc -l | tr -d ' ')
grep -q "doctest suites ($suites files" CLAUDE.md || report "CLAUDE.md test-suite count disagrees with tests/ ($suites .cpp files)"

if [ "$fail" -eq 0 ]; then
  echo "check-docs: OK"
fi
exit "$fail"
