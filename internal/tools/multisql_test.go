package tools

import "testing"

func TestDSNAllowedExactMatch(t *testing.T) {
	allow := []string{"host=readonly.db port=5432 user=ro", "postgres://ro@replica:5432/app"}
	if !dsnAllowed("host=readonly.db port=5432 user=ro", allow) {
		t.Error("an exact-match DSN should be allowed")
	}
	// Substring of an allowed entry inside a different DSN must NOT be allowed.
	if dsnAllowed("host=evil.db password=host=readonly.db port=5432 user=ro", allow) {
		t.Error("substring/semantic-escape DSN must be denied")
	}
	if dsnAllowed("host=prod.db", allow) {
		t.Error("non-matching DSN must be denied")
	}
	if dsnAllowed("anything", nil) {
		t.Error("empty allowlist should deny all (fail closed)")
	}
}

func TestMultiSQLRejectsWrites(t *testing.T) {
	tool := MultiSQL([]string{"host=x"})
	for _, q := range []string{
		`{"engine":"postgres","dsn":"host=x","query":"DELETE FROM t"}`,
		`{"engine":"mysql","dsn":"host=x","query":"UPDATE t SET a=1"}`,
	} {
		if _, err := run(t, tool, q); err == nil {
			t.Errorf("write query should be rejected: %s", q)
		}
	}
}

func TestMultiSQLRejectsDataModifyingCTE(t *testing.T) {
	// A data-modifying CTE (Postgres) must be rejected despite the leading WITH.
	q := `{"engine":"postgres","dsn":"host=x","query":"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d"}`
	if _, err := run(t, MultiSQL([]string{"host=x"}), q); err == nil {
		t.Error("data-modifying CTE must be rejected")
	}
}

func TestMultiSQLDeniesUnlistedDSN(t *testing.T) {
	if _, err := run(t, MultiSQL([]string{"host=allowed"}), `{"engine":"postgres","dsn":"host=evil","query":"SELECT 1"}`); err == nil {
		t.Error("a DSN not in the allowlist must be rejected")
	}
}

func TestMultiSQLUnknownEngine(t *testing.T) {
	if _, err := run(t, MultiSQL([]string{"x"}), `{"engine":"oracle","dsn":"x","query":"SELECT 1"}`); err == nil {
		t.Error("unknown engine should error")
	}
}
