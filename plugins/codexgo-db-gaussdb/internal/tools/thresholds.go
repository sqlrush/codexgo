package tools

// Shared constants for the monitoring tools.

// userSchemaPred is the full "exclude internal schemas" predicate for queries
// that expose a `schemaname` column (pg_stat_user_tables / all_tables). It
// reuses the schema list from index.go's sysSchemaFilter so the exclusion set
// stays single-sourced.
const userSchemaPred = `schemaname NOT IN (` + sysSchemaFilter + `)`

// xidWraparound is the 32-bit transaction-id space; XID age is measured against
// it to estimate wraparound risk.
const xidWraparound = 2147483647.0
