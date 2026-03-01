package store

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestRunMigrations_Fresh(t *testing.T) {
	conn := openTestDB(t)
	if err := runMigrations(conn); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// All expected tables should exist.
	for _, table := range []string{"directories", "tags", "videos", "video_tags", "schema_migrations"} {
		var count int
		conn.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&count)
		if count != 1 {
			t.Errorf("expected table %q to exist after migration", table)
		}
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	conn := openTestDB(t)
	if err := runMigrations(conn); err != nil {
		t.Fatalf("first runMigrations: %v", err)
	}
	versions1, err := ListMigrations(conn)
	if err != nil {
		t.Fatalf("ListMigrations after first run: %v", err)
	}

	if err := runMigrations(conn); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
	versions2, err := ListMigrations(conn)
	if err != nil {
		t.Fatalf("ListMigrations after second run: %v", err)
	}

	// Running twice should not add more migrations.
	if len(versions1) != len(versions2) {
		t.Errorf("expected same migration count after double run: first=%v second=%v", versions1, versions2)
	}
	// First migration should always be 001_initial.
	if len(versions1) == 0 || versions1[0] != "001_initial" {
		t.Errorf("expected first migration 001_initial, got %v", versions1)
	}
}
