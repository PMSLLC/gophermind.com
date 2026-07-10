package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// multiSQLDrivers maps a friendly engine name to its database/sql driver.
var multiSQLDrivers = map[string]string{"postgres": "postgres", "postgresql": "postgres", "mysql": "mysql"}

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

// dsnAllowed reports whether dsn matches any allowlist pattern (substring). An
// empty allowlist denies everything (fail closed).
func dsnAllowed(dsn string, allow []string) bool {
	for _, p := range allow {
		if p != "" && strings.Contains(dsn, p) {
			return true
		}
	}
	return false
}
