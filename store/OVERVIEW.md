# store — Database Layer

Package `store` defines the persistence interface and its SQLite implementation. All database interaction in the application goes through this layer.

## Purpose

Abstracts SQLite behind a `Store` interface so handlers never touch SQL directly. Handles schema migrations automatically on startup. Uses WAL mode and connection pooling for concurrent read performance.

## Files

| File | Responsibility |
|---|---|
| `store.go` | `Store` interface, all model types, domain constants |
| `sqlite.go` | SQLite implementation of `Store` using `modernc.org/sqlite` (pure Go, no CGo) |
| `migrate.go` | Auto-migration runner; applies embedded SQL files in order |
| `migrations/*.sql` | 20 versioned SQL files, one per schema change |

## Key Types

### Store Interface

The `Store` interface has ~30 methods grouped by domain:

- **Directories**: `AddDirectory`, `GetDirectories`, `DeleteDirectory`, `RenameDirectory`
- **Videos**: `UpsertVideo`, `GetVideo`, `GetVideos`, `SearchVideos`, `DeleteVideo`, `UpdateVideo*`, `GetNextUnwatched*`
- **Tags**: `AddTag`, `GetTags`, `AddVideoTag`, `RemoveVideoTag`, `PruneOrphanTags`
- **Watch history**: `RecordWatch`, `BatchRecordWatch`, `GetProgress`, `MarkWatched`, `ClearProgress`, `GetRecentlyWatched`
- **Sessions**: `SaveSession`, `LoadSessions`, `DeleteSession`, `PruneExpiredSessions`
- **Settings**: `GetSetting`, `SaveSetting`
- **Misc**: `UpdateThumbnailPath`, `UpdateDuration`, `Close`

### Video

The central model. Key fields:

| Field | Type | Notes |
|---|---|---|
| `ID` | int64 | Primary key |
| `Filename` | string | Bare filename (no path) |
| `DirectoryPath` | string | Registered directory |
| `DisplayName` | string | User-facing title (may be empty → falls back to filename) |
| `ShowName` | string | Inferred or set via tags |
| `VideoType` | string | One of the `VideoTypes` constants |
| `Rating` | int | 0 = unrated |
| `SeasonNumber`, `EpisodeNumber` | int | TV episode coordinates |
| `ThumbnailPath` | string | Absolute path to JPEG thumbnail |
| `DurationS` | float64 | Duration in seconds |
| `ColorLabel` | string | UI color badge |
| `Watched` | bool | Explicit watched flag |

Helper methods on `Video`:
- `Title()` — returns `DisplayName` if set, otherwise `Filename`
- `FilePath()` — joins `DirectoryPath` and `Filename`
- `HasFields()` — true if any `VideoFields` are non-empty

### VideoFields

Structured descriptive metadata that overlaps with both native file metadata and tags:

```
Genre, SeasonNumber, EpisodeNumber, EpisodeTitle, Actors, Studio, Channel, AirDate
```

These fields are stored as columns on the videos table and also mirrored into the tag system (e.g. `actor:Winona Ryder`, `genre:Drama`).

### Domain Constants

| Constant | Values |
|---|---|
| `VideoTypes` | `TV`, `Movie`, `Concert`, `Vlog`, `Blog`, `YouTube` (→ hex color) |
| `VideoLabelColors` | `red`, `orange`, `yellow`, `green`, `blue`, `purple` (→ hex color) |

Validators: `IsValidVideoType(s)`, `IsValidColorLabel(s)`.

## SQLite Configuration

The DSN passes pragmas directly (avoids `PRAGMA` statements on connections that may not be the writer):

```
file:video_manger.db?_foreign_keys=1&_journal_mode=WAL&_busy_timeout=15000
```

- `foreign_keys=1` — enforces `ON DELETE CASCADE` for tags and watch history
- `journal_mode=WAL` — allows concurrent readers during writes; 8-connection pool
- `busy_timeout=15000` — waits up to 15 s before returning `SQLITE_BUSY`

Library sync also implements application-level retry (3 attempts, 100/200/400 ms backoff) for writes that hit transient lock contention.

## Video Upsert

`UpsertVideo` uses `INSERT ... ON CONFLICT(filename, directory_path) DO UPDATE` so re-scanning a directory never creates duplicate records. Only fields that are empty in the DB are overwritten, preserving manual edits.

## Migrations

`migrate.go` reads all `*.sql` files from `migrations/` (embedded at compile time), tracks applied versions in a `schema_migrations` table, and applies any missing ones in lexicographic order. The runner is called once at startup before any other DB operation.

### Migration History

| Version | Change |
|---|---|
| 001 | Initial schema (directories, tags, video_tags, videos) |
| 002 | `watch_history` table (position + timestamp) |
| 003 | Ratings column |
| 004 | `settings` key-value table |
| 005 | Performance indexes |
| 006 | FTS5 full-text search on filename + display_name |
| 007 | `sessions` table (auth persistence across restarts) |
| 008 | `original_filename` column (immutable import record) |
| 009 | `video_fields` (genre, season, episode, actors, studio, channel, air_date) |
| 010 | `thumbnail_path` column |
| 011 | Show organization (show_name, season_number via tag system) |
| 012 | `video_type` column |
| 013 | Tags as lingua franca (actors, genre, etc. → namespace:value tags) |
| 014 | Season tags mirrored from season_number column |
| 015 | `duration` column |
| 016 | `air_date` column |
| 017 | Indexes for `GetNextUnwatchedFromSearch` |
| 018 | `watched` boolean flag on watch records |
| 019 | `roku_enabled` setting seed |
| 020 | Watch history performance index |

## Tag Namespace Convention

Tags use `namespace:value` to encode structured fields in the flat tag system:

```
actor:Winona Ryder
genre:Drama
studio:Netflix
channel:Hulu
season:2
```

This lets the API and UI filter by structured criteria without extra columns. `store.go` splits and recombines these when marshaling `Video` from query results.

## References

- Used by: every handler in the root package, `library.go`
- Implemented by: `sqlite.go`
- Schema source of truth: `migrations/*.sql`
