package db

import (
	"strings"
	"testing"
)

func TestBuildDSNDefaults(t *testing.T) {
	dsn := buildDSN(Target{Host: "h", Port: 8000, User: "u", Password: "p"})
	for _, want := range []string{
		"host=h", "port=8000", "user=u", "password=p",
		"database=postgres",                       // default db
		"sslmode=disable",                         // default sslmode
		"application_name=codexgo",                // identity
		"default_query_exec_mode=simple_protocol", // gaussdb codec safety
		"connect_timeout=15",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN missing %q\nDSN: %s", want, dsn)
		}
	}
}

func TestBuildDSNExplicit(t *testing.T) {
	dsn := buildDSN(Target{Host: "h", Port: 5432, User: "u", Password: "p", Database: "mydb", SSLMode: "require"})
	if !strings.Contains(dsn, "database=mydb") {
		t.Errorf("expected database=mydb, got %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("expected sslmode=require, got %s", dsn)
	}
	if strings.Contains(dsn, "dbname=") {
		t.Errorf("gaussdb DSN must use database= not dbname=, got %s", dsn)
	}
}

func TestConnectValidatesTarget(t *testing.T) {
	c := New()
	bad := []Target{
		{Host: "", Port: 8000, User: "u"},
		{Host: "h", Port: 0, User: "u"},
		{Host: "h", Port: 70000, User: "u"},
		{Host: "h", Port: 8000, User: ""},
	}
	for _, tgt := range bad {
		if err := c.Connect(nil, tgt, "x"); err == nil {
			t.Errorf("expected validation error for %+v", tgt)
		}
	}
}

func TestCellToString(t *testing.T) {
	if got := cellToString(nil); got != "" {
		t.Errorf("nil -> %q want empty", got)
	}
	if got := cellToString([]byte("abc")); got != "abc" {
		t.Errorf("[]byte -> %q", got)
	}
	if got := cellToString(int64(42)); got != "42" {
		t.Errorf("int64 -> %q", got)
	}
}
