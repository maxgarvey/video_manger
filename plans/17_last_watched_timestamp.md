# Plan 17 — Last-Watched Timestamp on Video Rows

## Goal
Show how long ago each video was last watched directly in the library list
(e.g. "2h ago", "yesterday", "Jan 15").

## Changes

### store/store.go
- Add `ListWatchHistory(ctx) (map[int64]WatchRecord, error)` to the Store interface.

### store/sqlite.go
- Implement `ListWatchHistory`: SELECT video_id, position, watched_at FROM watch_history,
  return as `map[int64]WatchRecord`.

### main.go
- Add `reltime(s string) string` helper: parses SQLite datetime string, returns
  human-readable relative time ("just now", "5 mins ago", "yesterday", "Jan 2", etc.).
- Register `reltime` in the template FuncMap.
- `serveVideoList`: call `ListWatchHistory` and pass result as `History map[int64]WatchRecord`.

### templates/video_list.html
- In each video row button: if `index $.History .ID` has a WatchedAt, render a
  second line in muted text showing `reltime .WatchedAt`.
- Button becomes two-line (drop `white-space:nowrap`).

## Tests
- Store: `TestListWatchHistory` — verifies map contains correct WatchRecord after RecordWatch.
- Handler: `TestHandleVideoList_ShowsLastWatched` — records a watch, checks response contains relative timestamp text.
