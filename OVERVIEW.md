# video_manger — Root Package

The root Go package is the application core. It wires together all sub-packages, runs the HTTP servers, and houses the HTTP handlers.

## Purpose

A self-hosted video library manager for local and LAN use. A single binary serves a browser UI (HTTPS, port 8081), a Roku-compatible HTTP API (HTTP, port 8080), and a JSON API for third-party clients. There are no external service dependencies at runtime beyond optional `ffmpeg`/`ffprobe`/`yt-dlp` binaries.

## File Map

| File | Responsibility |
|---|---|
| `main.go` | CLI flags, startup, background goroutines, graceful shutdown |
| `server.go` | Chi router, auth middleware, server struct and state |
| `handlers.go` | Shared handler utilities (SSE writer, token generation, file helpers) |
| `handlers_videos.go` | Video CRUD, playback, progress tracking, quick-label modal |
| `handlers_directories.go` | Directory registration, yt-dlp download, filesystem browser |
| `handlers_conversion.go` | Format conversion, trim, delogo, USB export |
| `handlers_metadata.go` | File metadata edit, video fields, TMDB lookup, settings |
| `handlers_api.go` | JSON API (`/api/*`) for Roku and third-party clients |
| `handlers_roku.go` | Cast queue (web UI → Roku) |
| `library.go` | Directory scanner, metadata inference, sidecar JSON, thumbnail generation |
| `tls.go` | Self-signed ECDSA-P256 TLS certificate generation |

## Key Data Types

**`server` struct** (server.go) — shared state passed to every handler as a receiver:
- `store` — SQLite backend (see `store/`)
- `sessions` / `sessionsMu` — in-memory session token → expiry map
- `passwordHash` — optional bcrypt hash for auth gate
- `syncingDirs` — mutex-protected set of in-progress directory scans
- `convertSem` — buffered channel (size 2) limiting concurrent ffmpeg/yt-dlp jobs
- `jobs`, `convertJobs`, `moveJobs` — live job state for SSE streaming
- `castVideoID`, `castLastPoll`, `castMu` — Roku cast queue (one pending video, 30 s TTL)

## Startup Sequence

1. Parse CLI flags (`-db`, `-dir`, `-http-port`, `-https-port`, `-password`)
2. Open SQLite (auto-migrates schema on first run)
3. Generate or verify TLS cert/key co-located with the DB file
4. Restore persisted sessions from DB (logins survive restart)
5. Register optional startup directory; trigger initial sync
6. Register mDNS (`video-manger.local`) for Roku discovery if `roku_enabled=true`
7. Run background goroutines: `startLibraryPoller` (60 s), `startSessionPruner` (1 h)
8. Start plain HTTP on `--http-port` (Roku/LAN) and HTTPS/HTTP2 on `--https-port` (browser)
9. Block on `SIGTERM`/`SIGINT`; graceful shutdown with 2 s grace period

## Authentication Flow

Authentication is optional (enabled with `-password`). When active:

1. Every request passes through `authMiddleware` in server.go
2. `/login` and `/logout` are exempt
3. On login: bcrypt comparison → generate 256-bit random token → store in memory + DB → set `HttpOnly/Secure/SameSite=Strict` cookie (7-day TTL)
4. Sessions persist across restarts; the hourly pruner removes expired ones

## Directory Scan / Library Sync

`syncDir()` in library.go is the core discovery engine:

1. Walks the directory tree recursively (skips nested registered dirs)
2. Pre-loads known videos into a `map[filepath]Video` to skip redundant writes
3. For each video file: upsert DB record → apply sidecar JSON → infer show/type → probe duration → generate thumbnail
4. After the walk: prune stale DB entries whose files no longer exist on disk

`startLibraryPoller` re-runs all registered directories every 60 seconds.

## SSE Progress Pattern

Long-running jobs (conversion, yt-dlp download, trim, bulk move) all follow the same pattern:

1. Handler validates, acquires a semaphore slot, creates a buffered string channel, stores the job
2. Handler returns an HTML fragment containing an `<div>` that opens an SSE connection
3. Background goroutine sends progress lines to the channel (non-blocking, drops on overflow)
4. SSE handler reads the channel and writes `data:` events to the client
5. `scheduleJobCleanup()` closes the channel and removes the job after 10 minutes (allows late-connecting clients to read the result)

## Concurrency Model

- Max 2 concurrent ffmpeg/yt-dlp processes (`convertSem`)
- Directory syncs are sequential when triggered by the poller (avoids DB write contention)
- All shared maps are protected by `sync.Mutex` or `sync.RWMutex`
- SQLite WAL mode supports concurrent reads; the store layer retries on `SQLITE_BUSY`

## Constants

| Constant | Value | Purpose |
|---|---|---|
| `sessionTTL` | 7 days | Cookie and session lifetime |
| `sessionPruneEvery` | 1 hour | How often to evict expired sessions |
| `libraryPollEvery` | 60 s | Directory re-scan interval |
| `convertConcurrent` | 2 | Max parallel ffmpeg/yt-dlp processes |

## Embedded Assets

Templates (`templates/*.html`) and static files (`static/`) are embedded into the binary via `//go:embed` at compile time. No external file deployment is needed.

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `-db` | `video_manger.db` | Path to SQLite database |
| `-dir` | _(empty)_ | Directory to register on startup |
| `-http-port` | `8080` | Plain HTTP (Roku / LAN) |
| `-https-port` | `8081` | HTTPS/HTTP2 (browser) |
| `-password` | _(empty)_ | Password for UI auth (omit to disable) |

## References

- Sub-packages: [`store/`](store/OVERVIEW.md), [`metadata/`](metadata/OVERVIEW.md), [`transcode/`](transcode/OVERVIEW.md)
- Electron wrapper: [`electron/`](electron/OVERVIEW.md)
- Roku channel: [`roku/`](roku/OVERVIEW.md)
- CLI tool: [`cmd/populate/`](cmd/populate/OVERVIEW.md)
- Makefile targets: `build`, `electron`, `electron-dist`, `electron-install`, `roku`, `roku-deploy`
