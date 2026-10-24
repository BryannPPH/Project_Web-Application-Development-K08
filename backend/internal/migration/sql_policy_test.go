package migration

import (
	"errors"
	"testing"
)

func TestValidateSQLPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sql     string
		blocked bool
	}{
		{name: "multiple statements", sql: "CREATE TABLE widgets(id bigint); INSERT INTO widgets VALUES (1)"},
		{name: "semicolon in string", sql: "INSERT INTO widgets(value) VALUES ('a;b')"},
		{name: "transaction word in comment", sql: "-- COMMIT\nCREATE TABLE widgets(id bigint)"},
		{name: "function body", sql: "CREATE FUNCTION f() RETURNS void AS $$ BEGIN NULL; END $$ LANGUAGE plpgsql"},
		{name: "begin", sql: "BEGIN; CREATE TABLE widgets(id bigint)", blocked: true},
		{name: "start transaction", sql: "START TRANSACTION", blocked: true},
		{name: "commit after valid", sql: "SELECT 1; COMMIT", blocked: true},
		{name: "savepoint", sql: "SAVEPOINT unsafe", blocked: true},
		{name: "set transaction", sql: "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE", blocked: true},
		{name: "unterminated quote", sql: "SELECT 'oops", blocked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSQLPolicy(test.sql)
			if test.blocked != errors.Is(err, ErrSQLPolicy) {
				t.Fatalf("validateSQLPolicy() error = %v, blocked = %v", err, test.blocked)
			}
		})
	}
}

func TestValidateMySQLAtomicPolicy(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"SELECT 1",
		"INSERT INTO items VALUES ('CREATE TABLE hidden'); UPDATE items SET id=2; DELETE FROM items WHERE id=3",
		"INSERT INTO items(value) VALUES ('/*! this is data */')",
		"# CREATE TABLE ignored\nSELECT * FROM `order;items`",
	}
	for _, sql := range allowed {
		if err := validateMySQLAtomicPolicy(sql); err != nil {
			t.Errorf("validateMySQLAtomicPolicy(%q) error = %v", sql, err)
		}
	}
	blocked := []string{
		"CREATE TABLE items(id bigint)",
		"ALTER TABLE items ADD value text",
		"TRUNCATE items",
		"BEGIN; SELECT 1",
		"/*!80000 CREATE TABLE hidden(id bigint) */; SELECT 1",
	}
	for _, sql := range blocked {
		if err := validateMySQLAtomicPolicy(sql); !errors.Is(err, ErrSQLPolicy) {
			t.Errorf("validateMySQLAtomicPolicy(%q) error = %v, want ErrSQLPolicy", sql, err)
		}
	}
}

func TestAdvisoryKeysAreStableAndNamespaced(t *testing.T) {
	t.Parallel()
	firstA, firstB := advisoryKeys("8d728274-b56e-4f93-8893-5b02d76a58cb")
	secondA, secondB := advisoryKeys("8d728274-b56e-4f93-8893-5b02d76a58cb")
	upperA, upperB := advisoryKeys("8D728274-B56E-4F93-8893-5B02D76A58CB")
	otherA, otherB := advisoryKeys("3b0ff86d-092f-4e3d-a5d2-c393186b398a")
	if firstA != secondA || firstB != secondB {
		t.Fatal("advisory key mapping is not stable")
	}
	if firstA != upperA || firstB != upperB {
		t.Fatal("equivalent UUID spellings must share an advisory lock")
	}
	if firstA == otherA && firstB == otherB {
		t.Fatal("distinct database IDs unexpectedly share a key")
	}
}
