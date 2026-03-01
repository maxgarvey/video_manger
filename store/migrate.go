package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// runMigrations creates the schema_migrations tracking table if needed, then
// applies any unapplied numbered SQL files from the migrations/ directory in
// lexicographic (version) order. Each migration runs inside a transaction.
func runMigrations(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		ver := strings.TrimSuffix(e.Name(), ".sql")
		var count int
		if err := conn.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, ver,
		).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", ver, err)
		}
		if count > 0 {
			continue // already applied
		}

		script, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", ver, err)
		}

		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", ver, err)
		}
		if _, err := tx.Exec(string(script)); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("apply migration %s: %w", ver, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`, ver,
		); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("record migration %s: %w", ver, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", ver, err)
		}
	}
	return nil
}

// ListMigrations returns all applied migration versions in the order they
// were applied. Useful for diagnostics / tests.
func ListMigrations(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
