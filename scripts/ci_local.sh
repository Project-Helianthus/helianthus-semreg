#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

if git grep -nE '[[:blank:]]+$' -- '*.go' '*.md' '*.sh' '*.yml' 'go.mod'; then
  echo "tracked source files contain trailing whitespace" >&2
  exit 1
fi

go_files=()
while IFS= read -r file; do
  go_files+=("$file")
done < <(find . -type f -name '*.go' -not -path './.git/*' -print | sort)

if ((${#go_files[@]} == 0)); then
  echo "no Go source files found" >&2
  exit 1
fi

unformatted=$(gofmt -l "${go_files[@]}")
if [[ -n "$unformatted" ]]; then
  echo "Go files require formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

env GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum

module_list=$(env GOWORK=off go list -m all)
unexpected_modules=$(printf '%s\n' "$module_list" |
	grep -Ev '^(github.com/Project-Helianthus/helianthus-semreg|golang.org/x/mod v0\.17\.0|golang.org/x/sync v0\.11\.0|golang.org/x/text v0\.22\.0|golang.org/x/tools v0\.21\.1-0\.20240508182429-e35e4ccd0d2d)$' || true)
if [[ -n "$unexpected_modules" ]]; then
	echo "semantic foundation has undeclared external module dependencies:" >&2
	echo "$unexpected_modules" >&2
	exit 1
fi

non_module_imports=$(env GOWORK=off go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... |
	grep -Ev '^(github\.com/Project-Helianthus/helianthus-semreg($|/)|golang\.org/x/text($|/))' || true)
if [[ -n "$non_module_imports" ]]; then
  echo "semantic packages import non-module code:" >&2
  echo "$non_module_imports" >&2
  exit 1
fi

env GOWORK=off go vet ./...
env GOWORK=off go test -race -count=1 -v ./...
env GOWORK=off go build ./...
