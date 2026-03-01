# Plan: Single watch_history scan in serveVideoList (#36)

`serveVideoList` currently calls both `ListWatchedIDs` (returns `map[int64]bool`) and
`ListWatchHistory` (returns `map[int64]WatchRecord`) — two full table scans.

The `Watched` map is only used in `video_list.html` to show the ✓ badge.
`ListWatchHistory` already returns all the same rows plus the timestamp.

## Fix

Drop `ListWatchedIDs` from `serveVideoList`. In `video_list.html`, derive the
"watched" boolean from the History map: a video is watched if its WatchRecord has a
non-empty `WatchedAt` field.

Change the template data struct field from separate `Watched map[int64]bool` to just
using `History map[int64]store.WatchRecord` already in the struct.

Update `video_list.html`: replace `{{if index $.Watched .ID}}` with
`{{if (index $.History .ID).WatchedAt}}`.
