# Video Manager Copilot Instructions

## Architecture Overview
This is a Go web application serving videos locally with a browser-based player. Key components:
- **Server struct** (`server.go`): Holds shared state (store, sessions, background jobs)
- **Store interface** (`store/store.go`): Abstracts data persistence; SQLite implementation via sqlc
- **Handlers** (`handlers_*.go`): HTTP endpoints using chi router; follow pattern `(s *server) func(w http.ResponseWriter, r *http.Request)`
- **Templates** (`templates/`): HTMX-powered HTML partials embedded in binary
- **Metadata** (`metadata/`): ffprobe/ffmpeg integration for file metadata read/write
- **Transcode** (`transcode/`): ffmpeg-based video conversion utilities

## Handler Patterns
- Use `s.store` for database operations (e.g., `s.store.GetVideo(r.Context(), id)`)
- Validate parameters with `parseIDParam(w, r)` and `videoOrError(w, r)` helpers
- Render templates with `render(w, "template.html", data)`
- Handle errors with `http.Error(w, msg, status)` or `slog.Error()`
- Background jobs (conversions, downloads) use channels for progress streaming

## Database Workflow
- Schema in `db/schema.sql`; migrations in `store/migrations/`
- Queries in `db/query.sql`; regenerate Go code with `sqlc generate`
- Use transactions for multi-step operations (e.g., `DeleteDirectoryAndVideos`)

## Development Commands
- **Test**: `go test ./... -race -count=1 -timeout 60s` (includes race detection)
- **Build**: `go build -o video_manger .`
- **Format**: `gofmt -w -s .` or `make fmt`
- **Regenerate DB code**: `sqlc generate` after schema/query changes

## Key Conventions
- **Sessions**: Token-based auth with 7-day TTL; persisted in DB when password enabled
- **File paths**: Store `directory_path` directly in videos for resilience (directories can be unlinked)
- **Ratings**: 0=neutral, 1=liked, 2=double-liked
- **Tags**: Many-to-many via `video_tags` table; prune orphans after untagging
- **Watch history**: Tracks position; `WatchedAt` populated via LEFT JOIN in list queries
- **ffmpeg optional**: Metadata features gracefully skip if binaries unavailable

## Example Handler
```go
func (s *server) handlePlayer(w http.ResponseWriter, r *http.Request) {
    video, ok := s.videoOrError(w, r)
    if !ok { return }
    tags, err := s.store.ListTagsByVideo(r.Context(), video.ID)
    if err != nil { http.Error(w, err.Error(), http.StatusInternalServerError); return }
    render(w, "player.html", map[string]any{"Video": video, "Tags": tags})
}
```

## Integration Points
- **HTMX**: Use `hx-*` attributes for dynamic UI; partial templates for panels/overlays
- **MDNS**: Automatic service discovery (e.g., `video-manger.local`)
- **External APIs**: TMDB for metadata lookup; yt-dlp for video downloads
- **File operations**: Use `filepath.WalkDir` for directory scanning; respect symlinks</content>
<parameter name="filePath">/Users/maxgarvey/go/src/github.com/maxgarvey/video_manger/.github/copilot-instructions.md