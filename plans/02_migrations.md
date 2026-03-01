# Plan: Formalize Migrations (Feature 2)

## Behaviour Change

Replace the ad-hoc `migrateVideos` function with a proper numbered
migration system:

- `migrations/` directory contains `001_initial.sql`, `002_*.sql`, …
- A `schema_migrations` table tracks which migrations have been applied.
- `applySchema` is replaced by `runMigrations` which applies only the
  unapplied migrations in order.
- Migrations are embedded via `embed.FS` so no external files needed at runtime.
- Each migration is applied in a transaction; failure rolls back and returns error.

## Migration files

`migrations/001_initial.sql` — the current schema (directories, tags,
video_tags, videos with directory_path column). Written to capture the
state after all previous ad-hoc migrations.

## Implementation

### New file: `store/migrate.go`

```go
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

func runMigrations(conn *sql.DB) error {
    // Ensure schema_migrations table exists.
    if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version TEXT PRIMARY KEY,
        applied_at TEXT NOT NULL DEFAULT (datetime('now'))
    )`); err != nil {
        return fmt.Errorf("create schema_migrations: %w", err)
    }

    entries, _ := migrationFS.ReadDir("migrations")
    sort.Slice(entries, func(i, j int) bool {
        return entries[i].Name() < entries[j].Name()
    })

    for _, e := range entries {
        ver := strings.TrimSuffix(e.Name(), ".sql")
        var count int
        conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, ver).Scan(&count)
        if count > 0 {
            continue
        }
        sql, _ := migrationFS.ReadFile("migrations/" + e.Name())
        tx, err := conn.Begin()
        if err != nil {
            return fmt.Errorf("begin tx for %s: %w", ver, err)
        }
        if _, err := tx.Exec(string(sql)); err != nil {
            tx.Rollback()
            return fmt.Errorf("apply migration %s: %w", ver, err)
        }
        if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, ver); err != nil {
            tx.Rollback()
            return fmt.Errorf("record migration %s: %w", ver, err)
        }
        if err := tx.Commit(); err != nil {
            return fmt.Errorf("commit migration %s: %w", ver, err)
        }
    }
    return nil
}
```

### `store/sqlite.go`

- Replace `applySchema` + `migrateVideos` with a call to `runMigrations`.
- `migrations/` directory lives under `store/`.

### Migration file: `store/migrations/001_initial.sql`

Contains CREATE TABLE IF NOT EXISTS for all tables in their current
final form.

## Important: existing databases

Existing databases already have the full schema. Since `001_initial.sql`
uses `CREATE TABLE IF NOT EXISTS`, it is safe to apply to an existing DB
(it's a no-op for existing tables). The `schema_migrations` row is then
inserted to mark it as applied. Future migrations start from 002.

## Tests

- `TestRunMigrations_Fresh` — new in-memory DB gets all tables.
- `TestRunMigrations_Idempotent` — running migrations twice doesn't error.
- The existing store tests continue to pass (NewSQLite still works).
