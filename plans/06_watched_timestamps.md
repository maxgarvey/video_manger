# Plan: Track Watched Timestamps (Feature 6)

## Behaviour Change

- When a user plays a video, record a `watched_at` timestamp and
  the playback position (seconds) in the database.
- The info panel shows "Last watched: <date>" for videos that have
  been watched.
- The video player resumes from the last known position when
  replaying a video.
- The video list shows a "watched" indicator (dimmed or checkmark)
  for recently watched videos.

## Schema

New migration: `store/migrations/002_watch_history.sql`

```sql
CREATE TABLE IF NOT EXISTS watch_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    position   REAL    NOT NULL DEFAULT 0,  -- seconds
    watched_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(video_id)  -- one row per video, updated on each watch
);
```

Using `UNIQUE(video_id)` with `INSERT OR REPLACE` keeps one row per
video, always the most recent watch.

## Store interface additions

```go
// In store.Store:
RecordWatch(ctx context.Context, videoID int64, position float64) error
GetWatch(ctx context.Context, videoID int64) (WatchRecord, error)
```

```go
type WatchRecord struct {
    VideoID   int64
    Position  float64
    WatchedAt string
}
```

## API

```
POST /videos/{id}/progress   body: position=<seconds>
GET  /videos/{id}/progress   returns: {"position": <seconds>, "watched_at": "<date>"}
```

The player uses `timeupdate` events to POST progress every 5 seconds
and on pause/unload. On load, it fetches the last position and seeks.

## UI changes

### `player.html`
- On video load, `GET /videos/{id}/progress` and seek to returned position.
- On `timeupdate` (throttled to every 5s) and `pause`/`beforeunload`,
  `POST /videos/{id}/progress` with current position.

### `video_list.html` — show watched indicator
- Pass `WatchMap map[int64]bool` to the template from `serveVideoList`.
- Show a small "✓" badge on watched videos.

### `file_metadata.html` — show last watched date
- Add watched_at to the metadata display.

## Tests

- `TestRecordAndGetWatch`
- `TestHandleProgress_Post`
- `TestHandleProgress_Get`
- `TestVideoList_ShowsWatchedIndicator`
