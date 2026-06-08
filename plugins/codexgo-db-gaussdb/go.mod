// codexgo-db-gaussdb is a standalone GaussDB MCP server plugin for codexgo.
//
// It is an INDEPENDENT Go module (its own go.mod) — codexgo's module does not
// import it, and it does not import codexgo. The only link is the MCP protocol
// (stdio JSON-RPC) that codexgo speaks to it as a client. This enforces the
// "domain business zero into codexgo core" invariant from PLUGIN-DB-DESIGN.
module github.com/sqlrush/codexgo-db-gaussdb

go 1.25

require (
	github.com/HuaweiCloudDeveloper/gaussdb-go v1.0.0-rc1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)
