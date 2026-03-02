# Self-Review — Round 9 Action Items

Conducted after implementing Rounds 1–8. Issues are grouped by category and
ordered by severity within each group. Items marked **(easy)**, **(medium)**,
or **(hard)** indicate relative implementation effort.

---

## 1 — BUGS

### 1a — XSS in handleUpdateVideoName (HIGH)
`handlers.go:150` — `w.Write([]byte(video.Title()))` writes the raw,
unescaped title directly to the response. HTMX swaps this into the
`innerHTML` of the `<span id="video-title-*">` element. A title containing
`<img src=x onerror=alert(1)>` or `<script>` tags will execute in the
browser.
**Fix:** Use `html.EscapeString(video.Title())` before writing, or render a
tiny template fragment. **(easy)**

### 1b — Stale DB record after cross-device move failure (HIGH)
`handlers.go:571–581` — `os.Rename` fails (cross-device), so `copyFile` is
called. After a successful copy, `os.Remove(src)` is called without checking
its error (`//nolint:errcheck`). Then `UpdateVideoPath` is called. If either
remove or DB update fails, the file exists in two places and the DB points to
the destination while the source is still there. There is no rollback.
**Fix:** Check `os.Remove` error and log it; if `UpdateVideoPath` fails,
attempt to move the file back and return an error to the user. **(medium)**

### 1c — yt-dlp metadata tagging fails for merged formats (MEDIUM)
`handlers.go:818–824` — The destination path is captured by watching for
`[download] Destination:`. However when yt-dlp downloads separate video +
audio streams and merges them (common for YouTube 1080p+), it emits:
```
[download] Destination: video.webm   ← caught (wrong file)
[download] Destination: audio.m4a    ← overwrites captured path (wrong)
[Merger] Merging formats into "video.mp4"
```
The captured path ends up being the audio stream, not the merged output.
The `.info.json` file uses the merged title, so `videoPath + ".info.json"`
is wrong.
**Fix:** Also watch for `[Merger] Merging formats into "..."` lines and
prefer that path over `[download] Destination:`. **(easy)**

### 1d — Original filename always shown even when unchanged (MEDIUM)
`templates/player.html:156–158` — The "Original: …" hint is shown whenever
`OriginalFilename` is non-empty. Because migration 008 backfills all existing
rows with `original_filename = filename`, and new imports always set
`original_filename = filename`, this hint is always visible and always shows
the same value as the current filename until the user renames the video. It
provides no information until a rename has actually occurred.
**Fix:** Change the condition to
`{{if and .Video.OriginalFilename (ne .Video.OriginalFilename .Video.Filename)}}`.
**(easy)**

### 1e — Rename doesn't update library sidebar immediately (MEDIUM)
`handlers.go:130–151` — After a successful rename the response updates only
the `<span id="video-title-*">` element via `hx-target`. The library list
`#video-list` still shows the old name until the 60-second polling refresh.
**Fix:** Also trigger a video list refresh. The form could use an out-of-band
swap or the handler can return both fragments. **(medium)**

### 1f — serveVideoList silently eats ListWatchHistory error (LOW)
`handlers.go:399` — `history, _ := s.store.ListWatchHistory(...)` discards
the error. If the watch_history table query fails (e.g., disk full, locked
DB), all videos appear as unwatched without any indication to the user.
**Fix:** Log the error; don't discard it silently. **(easy)**

### 1g — handleUpdateVideoFields does not sync to file metadata (MEDIUM)
`handlers.go:1186–1218` — Writing genre/season/episode/actors/studio/channel
to the DB via `UpdateVideoFields` does not embed those values into the video
file's native metadata via `metadata.Write`. The "File Metadata" section
(from ffprobe) and the new "Video Fields" section will therefore show
inconsistent data for the same video.
**Fix:** After DB save, call `metadata.Write` with the relevant fields
(genre, show/channel, season_number, episode_sort). **(easy)**

### 1h — handleMoveVideo: UpdateVideoPath error not surfaced to user (LOW)
`handlers.go:579–581` — If `UpdateVideoPath` fails (e.g. UNIQUE constraint),
the error is only logged; the response still renders the video list as if
nothing went wrong. The file has already moved on disk.
**Fix:** Return an error to the client and attempt to move the file back.
**(medium)**

---

## 2 — PERFORMANCE

### 2a — Full video fetch before SQL-level pagination (HIGH)
`handlers.go:330–397` — `serveVideoList` fetches **all** videos from the DB
(`ListVideos`, `SearchVideos`, `ListVideosByTag`, etc.), sorts them in Go,
then slices for pagination. For a library with thousands of videos this
loads the entire table into memory on every render plus every 60-second poll.
**Fix:** Move sorting + pagination into SQL (ORDER BY + LIMIT/OFFSET in all
list queries). **(hard)**

### 2b — ListWatchHistory on every video list render (MEDIUM)
`handlers.go:399` — The full `watch_history` table is fetched into a map on
every call to `serveVideoList`, including the automatic 60-second poll. For
large libraries this is unnecessary overhead.
**Fix:** Join `watch_history` in the video list query itself rather than
loading the whole table separately. **(medium)**

### 2c — In-Go sort duplicates what SQL already does (MEDIUM)
`handlers.go:361–376` — `slices.SortFunc` re-sorts the result set after the
DB already returned them in `ORDER BY` order. Because sort order is applied
post-fetch in Go, the DB index is not used.
**Fix:** Consolidate into SQL ORDER BY and remove the Go sort. Ties in with
2a. **(medium)**

### 2d — handlePlayer makes 4 sequential DB round-trips (LOW)
`handlers.go:87–115` — `GetVideo`, `ListTagsByVideo`, `ListTags`, `GetSetting`
are called one after another. Under SQLite WAL mode these are fast, but it's
worth noting if latency becomes an issue.
**Fix:** Low priority; revisit if player load time is ever noticeable.
**(medium)**

### 2e — syncDir UpsertVideo calls are not batched (LOW)
`handlers.go` (`syncDir`) — Each file in a scanned directory is upserted
individually (`UpsertVideo`). A directory with 2 000 files generates 2 000
separate SQL statements. Not a problem today but worth knowing.
**Fix:** Use a bulk INSERT with ON CONFLICT in a single transaction.
**(hard)**

---

## 3 — TEST COVERAGE

### 3a — New video fields endpoints have no tests (HIGH)
`handlers.go:1172–1218` — `handleGetVideoFields`, `handleEditVideoFields`,
`handleUpdateVideoFields` were added in Round 7 with no accompanying tests.
**Fix:** Add handler-level tests for GET (view), GET (edit form), and PUT
(save + return view) — including the zero-value fields case. **(easy)**

### 3b — yt-dlp metadata tagging: no integration test (MEDIUM)
The `parseYTDLPInfoJSON` function is unit-tested, but the full flow (capture
destination path → read .info.json → write to file → delete .info.json) has
no test.
**Fix:** A test that creates a fake `*.info.json` alongside a real video file
and verifies the metadata write path. **(medium)**

### 3c — handleExportUSB has no test (MEDIUM)
`handlers.go` (`handleExportUSB`) — No test verifies the handler returns the
file correctly or rejects invalid IDs.
**Fix:** Add tests for valid export, video not found, and missing-file cases.
**(easy)**

### 3d — handleListDuplicates has no test (LOW)
The duplicate detection handler has no test; the underlying logic is
exercised only by the store test.
**Fix:** Add a handler test seeding known duplicates. **(easy)**

### 3e — serveVideoList pagination has no test (LOW)
The page/limit slicing logic has no dedicated test.
**Fix:** Test that page=2 returns the correct window and that out-of-range
pages return empty results. **(easy)**

### 3f — Rename flow has no test for title appearing in response (LOW)
`TestHandleUpdateVideoName` (if it exists) likely only checks the HTTP
status, not that the response body contains the correct (escaped) title.
**Fix:** Assert response body equals expected HTML-escaped title. **(easy)**

---

## 4 — UX

### 4a — Download queue accumulates infinitely (MEDIUM)
`templates/index.html` — The yt-dlp download queue uses `hx-swap="beforeend"`
so each submit appends new job blocks. After many sessions the list grows
without bound until the user manually clicks "Clear". Completed jobs from
previous page loads are lost on refresh anyway.
**Fix:** Limit the visible queue to the most recent N (e.g. 10) job blocks;
auto-remove completed blocks after 30 seconds. **(easy)**

### 4b — No feedback after video rename in library sidebar (MEDIUM)
After rename the `<span>` in the info panel updates but the library list
keeps showing the old name. Users expect immediate consistency.
**Fix:** Tied to bug 1e — trigger a video list morph after rename. **(medium)**

### 4c — Video fields panel flashes empty on load (LOW)
`templates/player.html` — The `<div hx-get=".../fields" hx-trigger="load"
hx-swap="outerHTML">` starts as an empty div, creates a brief gap in the
info panel before the response arrives.
**Fix:** Replace the outer div's `hx-swap="outerHTML"` with
`hx-target="#video-fields-{{.Video.ID}}"` and give the div a stable ID,
or add a loading skeleton. **(easy)**

### 4d — "Original filename" shown before any rename (LOW)
Covered by bug 1d — the label appears for every video even when never
renamed, providing no value. Removing it for unchanged filenames is a UX
improvement too.

### 4e — yt-dlp textarea not cleared after submit (LOW)
`templates/index.html` — After submitting URLs the textarea retains its
content. Users might accidentally re-submit the same URLs.
**Fix:** Clear the textarea via `hx-on::after-request="this.reset()"` on the
form. **(easy)**

### 4f — "Connection lost" message ambiguous during yt-dlp downloads (LOW)
`templates/ytdlp_progress.html` — If the SSE connection drops (browser
tab goes to background, network hiccup) the message says "Connection lost"
even if the server-side download is still running fine.
**Fix:** Change message to "Connection lost — download may still be running
in background." **(easy)**

### 4g — Convert panel quality presets hidden but still submitted for mkv-copy (LOW)
`templates/player.html` — When "MKV — stream copy" is selected the quality
radio group is hidden via JS, but the browser still submits the selected
quality value. The handler ignores it for mkv-copy, so no wrong output, but
the hidden inputs are confusing form semantics.
**Fix:** Add `disabled` attribute to quality inputs when mkv-copy is selected
(JS). **(easy)**

### 4h — Video fields edit form: actors field needs placeholder clarification (LOW)
`templates/video_fields_edit.html` — The Actors input has
`placeholder="Comma-separated"` but gives no example. First-time users may
not know whether to use commas, semicolons, or newlines.
**Fix:** Change placeholder to `"e.g. Tom Hanks, Robin Wright"`. **(easy)**

---

## 5 — SECURITY

### 5a — XSS via unescaped title in handleUpdateVideoName (HIGH)
Duplicate of bug 1a — escalated here because it is also a security issue.
The raw title is injected into the page's DOM via HTMX innerHTML swap.
Mitigation: see fix in 1a.

### 5b — handleRelocateVideo accepts arbitrary filesystem paths (LOW)
`handlers.go:283–326` — The "relocate" form accepts any absolute path the
user types. This allows re-pointing a video record to any readable file on
the host (e.g. `/etc/shadow`). The video serve handler then serves that file
as bytes. In a single-user local setup this is low risk; on a shared/LAN
server it is a path traversal risk.
**Fix:** Restrict `newPath` to paths that are descendants of a registered
directory (same pattern as `handleBrowseFS`). **(easy)**

---

## Summary table

| # | Category | Severity | Effort | Item |
|---|----------|----------|--------|------|
| 1a | Bug/Security | HIGH | easy | XSS in handleUpdateVideoName |
| 1b | Bug | HIGH | medium | Stale DB after cross-device move failure |
| 1c | Bug | MEDIUM | easy | yt-dlp merged-format destination wrong |
| 1d | Bug/UX | MEDIUM | easy | Original filename shown when unchanged |
| 1e | Bug/UX | MEDIUM | medium | Rename doesn't refresh library sidebar |
| 1f | Bug | LOW | easy | ListWatchHistory error silently discarded |
| 1g | Bug | MEDIUM | easy | Video fields not synced to file metadata |
| 1h | Bug | LOW | medium | UpdateVideoPath error not surfaced |
| 2a | Perf | HIGH | hard | Full table fetch before pagination |
| 2b | Perf | MEDIUM | medium | Full watch history on every list render |
| 2c | Perf | MEDIUM | medium | Go sort duplicates SQL ORDER BY |
| 2d | Perf | LOW | medium | 4 sequential DB calls in handlePlayer |
| 2e | Perf | LOW | hard | syncDir UpsertVideo not batched |
| 3a | Tests | HIGH | easy | No tests for video fields endpoints |
| 3b | Tests | MEDIUM | medium | No integration test for yt-dlp tagging |
| 3c | Tests | MEDIUM | easy | No test for handleExportUSB |
| 3d | Tests | LOW | easy | No test for handleListDuplicates |
| 3e | Tests | LOW | easy | No test for pagination logic |
| 3f | Tests | LOW | easy | No assertion on rename response body |
| 4a | UX | MEDIUM | easy | Download queue grows unboundedly |
| 4b | UX | MEDIUM | medium | Library sidebar stale after rename |
| 4c | UX | LOW | easy | Video fields panel flashes empty |
| 4d | UX | LOW | easy | Original filename shown before rename |
| 4e | UX | LOW | easy | URL textarea not cleared after submit |
| 4f | UX | LOW | easy | Ambiguous SSE "Connection lost" message |
| 4g | UX | LOW | easy | Quality inputs still submitted for mkv-copy |
| 4h | UX | LOW | easy | Actors placeholder unclear |
| 5b | Security | LOW | easy | Relocate accepts arbitrary paths |
