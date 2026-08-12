#!/bin/zsh
# Mirror the CI gates (gofmt / vet / race tests) on the Mac host, which is the
# only machine that may build Go artifacts in the shared /Users/sqlrush tree.
# Usage: mac-ci-parity.sh [packages...]   (default: ./...)
set -eu
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
export GOFLAGS=-mod=readonly
cd /Users/sqlrush/codexgo

echo "== gofmt =="
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "not gofmt-clean:"; echo "$unformatted"; exit 1
fi
echo "clean"

echo "== vet =="
go vet ./...

echo "== test -race =="
if [ "$#" -gt 0 ]; then
  go test -race "$@"
else
  go test -race ./...
fi
