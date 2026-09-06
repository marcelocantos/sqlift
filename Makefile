# sqlift is built by `cv` (cvfile is the source of truth for the C++ build).
# This Makefile exists only to expose the owner gate — the single command the
# pre-push hook and CI both run.

.PHONY: gate hooks

gate:
	cv bundle
	git diff --quiet -- go/sqlift || (echo "cv bundle drift" && exit 1)
	cv test
	cv sanitize
	cd go/sqlift && CGO_LDFLAGS=-lsqlite3 go vet ./... && CGO_LDFLAGS=-lsqlite3 go test -count=1 ./...

hooks:
	git config core.hooksPath scripts/hooks
