# codexgo-db-gaussdb — independent MCP server plugin.
# Builds a stdio MCP binary into ./bin, which .mcp.json launches via
# ${CODEX_PLUGIN_ROOT}/bin/codexgo-db-gaussdb.

BIN := bin/codexgo-db-gaussdb
PKG := ./cmd/codexgo-db-gaussdb

.PHONY: all build vet test tidy clean smoke

all: build

build:
	go build -o $(BIN) $(PKG)

vet:
	go vet ./...

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin

# smoke runs the MCP initialize/tools-list handshake without a database.
smoke: build
	@printf '%s\n' \
		'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
		'{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
		| $(BIN)
