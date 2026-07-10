package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// multiSQLDrivers maps a friendly engine name to its database/sql driver.
var multiSQLDrivers = map[string]string{"postgres": "postgres", "postgresql": "postgres", "mysql": "mysql"}

// writeKeywordRe matches any data-modifying/DDL keyword as a whole word, used to
// reject writes hidden inside CTEs or multi-statement queries.
var writeKeywordRe = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|merge|replace|call|do|copy|lock|attach|vacuum|set)\b`)

// MultiSQL returns a read-only tool for querying Postgres/MySQL databases whose
// DSN matches the operator allowlist. Only SELECT/WITH statements run, so it can
// reason over real app databases without any risk of mutation.
func MultiSQL(dsnAllow []string) Tool {
	return Tool{
		Name:        "db_query",
		Description: "Run a READ-ONLY SELECT/WITH query against a Postgres or MySQL database (DSN must be operator-allowlisted). Returns rows.",
		Schema: object(map[string]any{
			"engine": str("Database engine: postgres or mysql."),
			"dsn":    str("Connection DSN (must match the configured allowlist)."),
			"query":  str("The SQL query (SELECT/WITH only)."),
		}, "engine", "dsn", "query"),
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Engine string `json:"engine"`
				DSN    string `json:"dsn"`
				Query  string `json:"query"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			driver, ok := multiSQLDrivers[strings.ToLower(a.Engine)]
			if !ok {
				return "", fmt.Errorf("unsupported engine %q (use postgres or mysql)", a.Engine)
			}
			if verb := firstVerb(a.Query); verb != "SELECT" && verb != "WITH" {
				return "", fmt.Errorf("only read-only SELECT/WITH queries are allowed (got %q)", verb)
			}
			// Defense in depth: Postgres allows data-modifying CTEs
			// (WITH x AS (DELETE ... RETURNING) SELECT ...), which the leading-verb
			// check alone would let through. Reject any write keyword anywhere.
			if writeKeywordRe.MatchString(a.Query) {
				return "", fmt.Errorf("query contains a data-modifying keyword; only pure reads are allowed")
			}
			if !dsnAllowed(a.DSN, dsnAllow) {
				return "", fmt.Errorf("DSN is not in the allowlist")
			}
			db, err := sql.Open(driver, a.DSN)
			if err != nil {
				return "", fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			rows, err := db.QueryContext(ctx, a.Query)
			if err != nil {
				return "", fmt.Errorf("query: %w", err)
			}
			defer rows.Close()
			return formatRows(rows)
		},
	}
}

// dsnAllowed reports whether dsn is an EXACT match for an allowlist entry.
// Substring matching would be bypassable — an allowed fragment could appear in a
// DSN (e.g. as a password/param) that actually connects elsewhere — so the
// operator must list the full connection strings. An empty allowlist denies
// everything (fail closed).
func dsnAllowed(dsn string, allow []string) bool {
	for _, p := range allow {
		if p != "" && dsn == p {
			return true
		}
	}
	return false
}
