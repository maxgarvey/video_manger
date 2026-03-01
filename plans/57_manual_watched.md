# Plan: Manual "mark as watched" from info panel (#57)

## What it means

"Manually input watching a video" = let the user mark a video as
watched (and optionally set a position) without actually playing it
through to that point.

## Backend

Reuse the existing `POST /videos/{id}/progress` endpoint — it already
accepts `position` and calls `RecordWatch`. No new handler needed.

## Frontend (`player.html`)

Add a small "Mark watched" action to the info panel, in the watch
history section. Two quick buttons:

- **Mark watched** — posts `position=1` (records a watch at second 1,
  which is enough to satisfy the "watched" check `position > 0`)
- **Clear watched** — posts `position=0` (resets the record)

After either action, refresh the `#video-list` so the ✓ indicator
updates, and refresh the progress display in the info panel itself.
