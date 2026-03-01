# Plan: Video not found — offer delete or relocate (#51)

When a video is in the DB but the file no longer exists on disk,
`handlePlayer` currently tries to serve a broken `<video>` tag.
Instead, detect the missing file and offer the user remediation.

## Backend

1. **`store.Store` interface** — add `UpdateVideoPath(ctx, id, dirID int64, dirPath, filename string) error`
2. **`store.SQLiteStore`** — implement with `UPDATE videos SET …`
3. **`handlePlayer`** — after `GetVideo`, call `os.Stat(video.FilePath())`.
   If the file is absent, set `FileNotFound: true` in the template data.
4. **New route** `POST /videos/{id}/relocate` → `handleRelocateVideo`
   - Reads `newpath` form field; stats the path to confirm it exists
   - Finds or creates a `directories` record for the parent dir
   - Calls `UpdateVideoPath`; then delegates to `handlePlayer` to re-render

## Frontend (`player.html`)

When `{{if .FileNotFound}}`, render a centred error panel instead of
the `<video>` tag:

```
⚠  File not found
/full/path/to/missing.mkv

[Remove from library]          <- hx-delete + closeTab JS
[Relocate ____________] →      <- hx-post /videos/{id}/relocate
```

The `#info-panel` OOB swap is emitted unconditionally so the side
panel remains functional.
