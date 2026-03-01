# video_manger

[![CI](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml)

A localhost web application for serving and playing videos in the browser.

## Overview

`video_manger` is a Go web server that runs locally and streams your video files through a browser-based player. Point it at a directory, open your browser, and watch — no external media players needed.

Directories and video metadata (custom names, tags) are persisted in a local SQLite database. If `ffprobe`/`ffmpeg` are installed, native file metadata is read on playback and can be edited and written back to the file through the UI.

## Usage

```bash
go run main.go -dir /path/to/videos -port 8080
```

Then open `http://localhost:8080` in your browser.

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | _(none)_ | Video directory to register on first run (optional) |
| `-db` | `video_manger.db` | Path to the SQLite database file |
| `-port` | `8080` | Port to listen on |

Directories can also be added and removed from the UI at any time.

## Features

- **Full-window player** — video fills the entire viewport; all controls appear as translucent overlays on hover
- **Library sidebar** — slides in from the left; lists directories, tag filters, and the video list with search
- **Info panel** — slides up from the bottom; shows the name editor, tags, and file metadata for the current video
- **Search** — type in the Videos section to filter the list in real time (matches display name or filename)
- **Custom names** — rename any video with a display name (stored in DB, written to the file's title tag if ffmpeg is available)
- **Tags** — add/remove tags per video; synced to the file's `keywords` metadata field
- **Filter by tag** — click a tag in the sidebar to filter the video list
- **Edit file metadata** — view and edit embedded metadata (title, description, genre, show, network, episode ID, season/episode number, date, comment) via ffmpeg — no re-encoding
- **Delete videos** — remove from the library only, or delete the file from disk too
- **Multiple directories** — register as many directories as you like; removing one cascades to its videos

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `L` | Toggle library sidebar |
| `I` | Toggle info panel |
| `Esc` | Close all panels |

Shortcuts are suppressed when an input or text area is focused.

## Supported Formats

`.mp4`, `.webm`, `.ogg`, `.mov`, `.mkv`, `.avi`

## Optional: ffmpeg / ffprobe

Install [ffmpeg](https://ffmpeg.org/download.html) to enable metadata read/write. Without it, the app works fully — metadata features are silently skipped.

```bash
# macOS
brew install ffmpeg
```

## Development

```bash
# Run tests (all packages, with race detector)
go test ./... -race

# Regenerate sqlc DB layer after changing db/schema.sql or db/query.sql
sqlc generate

# Build binary
go build -o video_manger .
```

## Architecture

```
main.go           — HTTP server, routes, handlers (server struct)
store/            — Store interface + SQLite implementation
  store.go        — Backend-agnostic interface and model types
  sqlite.go       — SQLite implementation via sqlc-generated db/ package
db/               — sqlc-generated code (schema.sql + query.sql → Go)
metadata/         — ffprobe read and ffmpeg write helpers
cmd/populate/     — one-time script to bulk-tag episodes via TVMaze API
templates/        — htmx-powered HTML partials (embedded in binary)
```

The `store.Store` interface makes the persistence layer swappable — a different backend (e.g. Postgres) just needs to implement the interface.

## Stack

- **[chi](https://github.com/go-chi/chi)** — idiomatic Go router (similar API to gorilla/mux, actively maintained)
- **[htmx](https://htmx.org)** — dynamic UI via HTML attributes, no JS framework
- **[sqlc](https://sqlc.dev)** — type-safe Go from SQL; generated code committed, no runtime dependency
- **[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)** — pure Go SQLite driver, no CGo
- **`net/http`** — range request support for video seeking
- **`embed`** — HTML templates compiled into the binary
- **ffmpeg / ffprobe** — optional, for native file metadata read/write
