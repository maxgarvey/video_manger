```
 ╔══════════════════════════════╗
 ║  ▶  v i d e o _ m a n g e r ║
 ╚══════════════════════════════╝
```

A self-hosted Go web server that turns a directory of video files into a
browser-based media library — no external media players, no cloud, no fuss.

[![CI](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml/badge.svg)](https://github.com/maxgarvey/video_manger/actions/workflows/ci.yml)

---

## What it does

Point it at a folder. Open a browser. Watch your videos.

- **Full-window player** with overlay controls; nothing visible until you hover
- **Library sidebar** — directory list, tag filters, search, video list
- **Metadata** — rename videos, tag them, edit embedded file metadata (title, show, season/episode, genre, actors) via ffmpeg without re-encoding
- **Watch history** — remembers where you left off; resumes on next play
- **Organisation** — groups TV episodes by show and season automatically
- **Conversion** — transcode to H.264/H.265/VP9 or remux to MKV from the UI
- **Trimming** — clip a time range out of any video (stream copy, no re-encode)
- **Thumbnails** — auto-generated at a random seek point via ffmpeg on sync
- **yt-dlp integration** — paste a URL, download directly into the library
- **Roku channel** — browse and stream over your LAN via the `roku/` BrightScript channel
- **JSON API** — `/api/*` endpoints for external clients (shows, seasons, episodes, tags, recently watched)

---

## Quick start

```bash
# build
go build -o video_manger .

# run (ffmpeg/ffprobe optional but recommended)
./video_manger -db video_manger.db -dir /path/to/videos
```

Open `http://localhost:8080`.

| Flag | Default | Description |
|------|---------|-------------|
| `-dir` | — | Video directory to register on first run |
| `-db` | `video_manger.db` | SQLite database path |
| `-port` | `8080` | Port to listen on |
| `-password` | — | Bcrypt-hash a password to enable basic auth |

Directories can be added and removed from the UI at any time. Removing a
directory offers the choice to keep or delete the files on disk.

**Keyboard shortcuts:** `L` library · `I` info panel · `Esc` close all

---

## Architecture

```
video_manger/
├── main.go                 entry point, flags, startup
├── server.go               server struct, route table, session auth
├── handlers_*.go           HTTP handlers grouped by feature area
├── library.go              directory sync, show/type inference, sidecar JSON
├── store/
│   ├── store.go            Store interface and model types
│   ├── sqlite.go           SQLite implementation
│   └── migrations/         SQL migration files (applied automatically)
├── metadata/               ffprobe read + ffmpeg write helpers
├── transcode/              ffmpeg conversion, trim, thumbnail generation
├── templates/              HTMX-powered HTML partials (embedded in binary)
└── roku/                   BrightScript Roku channel (see roku/README.md)
```

**Request flow:** `chi` router → auth middleware → handler → `store.Store` →
SQLite. Partial-page updates are driven by HTMX; the server returns HTML
fragments, not JSON (except the `/api/*` routes for external clients).

**Key dependencies**

| | |
|---|---|
| [chi](https://github.com/go-chi/chi) | HTTP router |
| [htmx](https://htmx.org) | Dynamic UI without a JS framework |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | Pure-Go SQLite (no CGo) |
| ffmpeg / ffprobe | Optional — metadata, conversion, thumbnails |

---

## Development

```bash
go test ./... -race   # full test suite with race detector
go build -o video_manger .
```
