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

- **Full-window player** with MTV-style lower-third overlay (show, episode, genre, actors, air date); fades in on hover
- **Library sidebar** — directory list, tag filters (randomly shuffled, 2-row cap), search, video list
- **Full-text search** — FTS5 trigram for ≥3 chars, LIKE fallback for shorter terms
- **Quick label** — one-click modal to set title, show, season/episode, genre, actors, studio, channel, air date, type and tags without leaving the player
- **Metadata** — edit embedded file metadata (title, show, season/episode, genre, actors, air date) via ffmpeg without re-encoding
- **Watch history** — remembers where you left off; resumes on next play; mark watched / clear watched
- **Ratings** — like (♥) or favourite (★) any video; filter the library by rating
- **Video types** — categorise as Movie, TV, Short, etc.; colour-coded badges; filter by type
- **Organisation** — groups TV episodes by show and season automatically
- **Conversion** — transcode to H.264/H.265/VP9 or remux to MKV from the UI; background job with SSE progress stream
- **Trimming** — clip a time range out of any video (stream copy, no re-encode); metadata and tags are copied to the new file
- **USB export** — one-click H.264/AAC MP4 optimised for USB stick playback
- **Thumbnails** — auto-generated at a random seek point via ffmpeg on sync; regenerate button per video
- **Color & audio tools** — draggable floating widgets with brightness/contrast/saturation/hue/sepia sliders plus 10 colour presets (Vivid, VHS, Noir, Film, …) and 9 audio EQ presets (Bass Boost, Classical, Cinematic, …); save unlimited custom presets to localStorage
- **yt-dlp integration** — paste a URL, download directly into the library
- **Roku channel** — browse shows/seasons/episodes with thumbnail grids, live search-as-you-type, info overlay with like/fav buttons, streams over LAN (`roku/`)
- **JSON API** — `/api/*` endpoints for external clients (shows, seasons, episodes, tags, search, recently watched)

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

**Keyboard shortcuts:** `L` library · `I` info panel · `→` random video · `Esc` close all

---

## Architecture

```
video_manger/
├── main.go                 entry point, flags, startup
├── server.go               server struct, route table, session auth
├── handlers_videos.go      video CRUD, quick-label, ratings, thumbnails
├── handlers_conversion.go  ffmpeg conversion, trim, USB export
├── handlers_api.go         JSON API (/api/* routes)
├── handlers_directories.go directory sync, yt-dlp download, filesystem browse
├── handlers_metadata.go    file metadata, video fields, tags list, TMDB lookup
├── library.go              directory sync, show/type inference, sidecar JSON
├── store/
│   ├── store.go            Store interface and model types
│   ├── sqlite.go           SQLite implementation (FTS5, migrations)
│   └── migrations/         SQL migration files (applied automatically)
├── metadata/               ffprobe read + ffmpeg write helpers
├── transcode/              ffmpeg conversion, trim, thumbnail generation
├── templates/              HTMX-powered HTML partials (embedded in binary)
└── roku/                   BrightScript Roku channel
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

## Roku channel

The `roku/` directory contains a BrightScript SceneGraph channel that streams
from the Go server over your LAN.

- Browse by show → season → episode with thumbnail grids
- Live search-as-you-type (500 ms debounce, FTS5 on server)
- Info overlay with like/fav buttons and air date
- Remembers watch progress

**Deploy:**
```bash
rm -f video_manger_roku.zip
make roku-deploy ROKU_IP=<device-ip> ROKU_PASS=rokudev
```

**Debug logs:**
```bash
nc <device-ip> 8085
```

---

## Development

```bash
go test ./... -race   # full test suite with race detector
go build -o video_manger .
```
