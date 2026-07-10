package tools

import "testing"

func TestDSNAllowed(t *testing.T) {
	allow := []string{"host=readonly.db", "@replica:"}
	if !dsnAllowed("host=readonly.db port=5432", allow) {
		t.Error("matching DSN should be allowed")
	}
	if !dsnAllowed("user:pw@replica:3306/app", allow) {
		t.Error("second-pattern DSN should be allowed")
	}
	if dsnAllowed("host=prod.db", allow) {
		t.Error("non-matching DSN must be denied")
	}
	// Empty allowlist denies everything (fail closed).
	if dsnAllowed("anything", nil) {
		t.Error("empty allowlist should deny all (fail closed)")
	}
}

func TestMultiSQLRejectsWrites(t *testing.T) {
	root := t.TempDir()
	tool := MultiSQL([]string{"host=x"})
	for _, q := range []string{
		`{"engine":"postgres","dsn":"host=x","query":"DELETE FROM t"}`,
		`{"engine":"mysql","dsn":"host=x","query":"UPDATE t SET a=1"}`,
	} {
		if _, err := run(t, tool, q); err == nil {
			t.Errorf("write query should be rejected: %s", q)
		}
	}
	_ = root
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
