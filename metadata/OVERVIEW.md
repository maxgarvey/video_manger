# metadata — File Metadata Read/Write

Package `metadata` wraps `ffprobe` (read) and `ffmpeg` (write) for accessing and modifying native video file metadata without re-encoding.

## Purpose

Reads metadata embedded in video files (title, genre, keywords, show/episode info) and writes updated metadata back using a stream-copy pass — no video or audio quality loss. Also probes stream-level codec information and file duration.

## Functions

### Read(path string) (Meta, error)

Calls `ffprobe -v quiet -print_format json -show_format` and parses the `format.tags` block.

Returns a `Meta` struct with fields:

| Field | ffprobe tag |
|---|---|
| `Title` | `title` |
| `Description` | `description` |
| `Genre` | `genre` |
| `Date` | `date` |
| `Keywords` | `keywords` (comma-split) |
| `Artist` | `artist` |
| `Comment` | `comment` |
| `Show` | `show` |
| `EpisodeID` | `episode_id` |
| `SeasonNum` | `season_number` |
| `EpisodeNum` | `episode_sort` |

If `ffprobe` is not installed, returns an empty `Meta` (graceful no-op; the library sync still functions without it).

---

### Write(path string, u Updates) error

Calls `ffmpeg -codec copy -map_metadata 0` with `-metadata key=value` flags for each non-nil field in `Updates`.

**Atomic write**: output goes to a temp file in the same directory; on success it is renamed over the source. On failure the temp file is removed and the source is untouched.

`Updates` fields are all pointers — `nil` means "leave unchanged", a pointer to `""` clears the field:

```go
type Updates struct {
    Title       *string
    Description *string
    Genre       *string
    Date        *string
    Comment     *string
    Keywords    []string   // nil = unchanged; empty slice = clear
    Show        *string
    EpisodeID   *string
    SeasonNum   *string
    EpisodeNum  *string
    Network     *string
}
```

If `ffmpeg` is not installed, returns `nil` (graceful no-op).

---

### ReadStreams(path string) ([]Stream, error)

Calls `ffprobe -v quiet -print_format json -show_streams` and returns one `Stream` per audio/video track:

| Field | Description |
|---|---|
| `CodecType` | `video` or `audio` |
| `CodecName` | `h264`, `aac`, `opus`, etc. |
| `Width`, `Height` | Video dimensions (video streams only) |
| `FrameRate` | Rational string, e.g. `24000/1001` |
| `BitRate` | bits/s as string |
| `SampleRate` | Hz (audio streams only) |
| `Channels` | Audio channel count |

Used by `handlers_metadata.go` to populate the stream info panel in the UI.

---

### ReadDuration(path string) (float64, error)

Calls `ffprobe -v quiet -print_format json -show_entries format=duration` and returns the duration in seconds as a float64.

Used by `library.go` to populate `Video.DurationS` during directory sync.

## Workflow: Metadata Round-Trip

```
File on disk
    │
    ▼
Read(path) ──────────────────────────────► Meta{Title, Genre, ...}
                                             displayed in UI

User edits fields in browser form
    │
    ▼
Write(path, Updates{Title: &"New Title"})
    │
    ├── ffmpeg -codec copy -metadata title="New Title" in.mp4 tmp.mp4
    └── rename tmp.mp4 → in.mp4
```

This same round-trip is used by:
- `handlers_metadata.go` (user editing file metadata in the UI)
- `library.go` (`applySidecar`, `syncTagsToFile`)
- `handlers_directories.go` (yt-dlp post-download metadata application)
- `handlers_conversion.go` (copying metadata to derived files after trim/delogo)

## External Dependencies

| Binary | Purpose | Behavior if missing |
|---|---|---|
| `ffprobe` | Read metadata and streams | Returns empty struct, no error |
| `ffmpeg` | Write metadata | Returns nil, no error |

The application calls `checkBinaries()` at startup (in `main.go`) to warn when these are absent, but does not exit — the library and UI remain functional for basic browsing without them.

## References

- Used by: `library.go`, `handlers_metadata.go`, `handlers_directories.go`, `handlers_conversion.go`
- Companion package for re-encoding: [`transcode/`](../transcode/OVERVIEW.md)
