# Self-Review: Shortcomings & Desired Changes

A candid assessment of the codebase after implementing all 26 Round 1+2 features.
Ordered by severity within each category.

---

## 1. Correctness Bugs

### 1a. Stale library entries when files are deleted from disk
`syncDir` only upserts new files — it never removes database rows for files that no
longer exist on disk. If you delete a video file externally, the entry stays in the
library. Clicking it will fail silently (the video element gets no data).
**Fix**: after walking the directory, compare the set of found filenames against
`ListVideosByDirectory`, and call `DeleteVideo` for anything missing.

### 1b. Info panel stuck on last-opened tab (multi-tab UX bug)
`player.html` populates `#info-panel` via `hx-swap-oob="true"`. All tabs share the
same `#info-panel` element. The OOB swap fires when a tab is _opened_, not when it
becomes _active_. Switching between already-open tabs (via `activateTab`) does not
update the panel, so the rating buttons, tags, rename form, etc. show the last-opened
video, not the currently-playing one.
**Fix**: on `activateTab`, either reload the info panel via htmx or keep a per-tab
copy of the info panel content and swap it in when switching.

### 1c. `handleConvert` partial output on context cancellation
`exec.CommandContext(r.Context(), ...)` means if the browser disconnects, ffmpeg is
killed mid-conversion. Unlike `handleTrim`, `handleConvert` (and `handleExportUSB`)
do not always clean up the partial output on error.
**Fix**: use `context.WithoutCancel` (Go 1.21) or a detached background context for
long-running ffmpeg jobs so they complete regardless of browser disconnect. Return a
job ID immediately and poll for completion.

### 1d. `handleTrim` fast-seek + `-c copy` produces keyframe-rounded start
`ffmpeg -ss <start> -i input -c copy` seeks to the nearest keyframe _before_ the
requested start time, so the actual cut point may be a few seconds early. This is
expected ffmpeg behaviour but surprises users who expect frame-accurate trimming.
**Fix**: document the caveat in the UI, or offer a "precise" mode that re-encodes
(`-c:v libx264 -crf 18`) for frame-accurate trimming.

### 1e. Same video in multiple tabs races on progress saves
If the same video ID is open in two tabs and both are playing, `sendBeacon` calls
race to `POST /videos/{id}/progress`. The last write wins, which could push an
earlier position onto a later one.
**Fix**: minor for a personal tool; document as a known limitation.

---

## 2. Security

### 2a. No authentication
The server has zero access control. Anyone on the same LAN (or any network reachable
by the machine) can browse the filesystem, download arbitrary URLs via yt-dlp, delete
files, and read your TMDB API key. The LAN-sharing feature actively advertises the
server address.
**Fix**: add an optional single-user password (stored as bcrypt hash in settings).
A simple HTTP Basic Auth middleware or a cookie-based session would be sufficient.
Even an optional `--password` flag would be a meaningful improvement.

### 2b. `handleBrowseFS` exposes entire filesystem
The directory-picker API (`GET /browse-fs?path=...`) accepts arbitrary paths after
`filepath.Clean`. There is no restriction to the user's home directory or registered
library paths. A LAN attacker can enumerate the full filesystem.
**Fix**: restrict the `path` query param to be either the home directory subtree or
an explicit allowlist of base directories. Return 403 for paths outside the allowed
roots.

### 2c. TMDB API key stored in plaintext
The key is stored as a plain string in the `settings` SQLite table and sent back to
the browser as a value inside `<input type="password">` (value attribute is present).
Any XSS or LAN attacker that can read the database or the settings page gets the key.
**Fix**: never echo the key back as an input value; show a placeholder indicating it
is set. The key only needs to be written, not read.

---

## 3. Dead Code

### 3a. `GET /play/random` / `handleRandomPlayer` is unreachable
The frontend tab system replaced the old `hx-get="/play/{id}" hx-target="#player"`
pattern. `handleRandomPlayer` is still registered and serving HTML into the now-gone
single-player structure. It can never work correctly now that `#player` contains the
tab container rather than a raw `<video>` slot.
**Fix**: remove the route and handler; the `/random-video` JSON endpoint is its
functional replacement.

### 3b. `strconv.Atoi(*port)` result discarded
In `main()`, `port` is parsed as an integer but the result is immediately thrown away
(`_ = ...`). The integer is never used; only the string `*port` is used in the
address. The parse was probably an early guard, but the error is silently ignored.
**Fix**: remove the `Atoi` call entirely, or add a proper validation step that logs
and exits on a non-numeric port string.

---

## 4. Performance

### 4a. `serveVideoList` makes two full `watch_history` scans per render
`ListWatchedIDs` scans the entire `watch_history` table, then `ListWatchHistory`
scans it again. Both return all rows. The list is rendered on every poll (every 60s
client-side), every search keystroke, and every tag filter click.
**Fix**: combine into a single query, or — since `ListWatchHistory` is a superset —
drop `ListWatchedIDs` and derive the watched set from the history map in the template
by checking `$w.WatchedAt`.

### 4b. `GetRandomVideo` full-table sort
`ORDER BY RANDOM() LIMIT 1` forces SQLite to sort the entire videos table. For a
personal library this is fine, but it can be a 100ms+ operation on large libraries.
**Fix**: `SELECT id FROM videos LIMIT 1 OFFSET (ABS(RANDOM()) % (SELECT COUNT(*) FROM videos))`
is O(offset) instead of O(n log n).

### 4c. `handleListDuplicates` `os.Stat` on every video on every call
The duplicate scan stats every file on disk on every invocation. There is no caching.
**Fix**: acceptable for an on-demand scan. Consider adding a loading spinner and
running it in a background goroutine if the library is large.

---

## 5. Architecture & Code Quality

### 5a. `startLibraryPoller` has no graceful shutdown
The goroutine is started with `go s.startLibraryPoller(ctx)` where `ctx` comes from
`context.Background()` in `main()`. The background context is never cancelled, so
the goroutine leaks when the server exits. There is no `http.Server.Shutdown` call
either — the process just `log.Fatal`s on `ListenAndServe`.
**Fix**: wire a proper signal handler (`signal.NotifyContext` on SIGTERM/SIGINT),
cancel the root context on shutdown, and call `httpServer.Shutdown(ctx)`.

### 5b. `handleDirectoryDeleteConfirm` uses `ListDirectories` + linear scan
`GetDirectory(ctx, id)` exists on the store; `handleDirectoryDeleteConfirm` ignores
it and fetches all directories then loops to find by ID.
**Fix**: replace with a direct `GetDirectory` call.

### 5c. `DupGroup` exported from `main` package
`DupGroup` is only used as a template data struct within the same package. Exporting
it from `main` follows no convention (nothing can import `main`).
**Fix**: make it unexported: `type dupGroup struct { ... }`.

### 5d. `handleAddDirectory` and `handleCreateDirectory` share ~80% of their body
Both validate `path`, call `AddDirectory`, call `syncDir`, and call `serveDirList`.
The only difference is the `os.MkdirAll` call in `handleCreateDirectory`.
**Fix**: extract a shared `addAndSyncDir(w, r, path string)` helper.

### 5e. TMDB handler logic is large and inline in `main.go`
`handleLookupApply` is ~70 lines handling two media types. The TMDB API structs and
`tmdbGet` helper have nothing to do with HTTP routing.
**Fix**: extract a `tmdb` internal package with `Search`, `MovieDetail`,
`EpisodeDetail` functions. Keeps `main.go` as a thin routing/handler layer.

### 5f. `metadata` package read/write requires `ffprobe`/`ffmpegprobe` to be on PATH
There is no check at startup for the presence of `ffmpeg` and `ffprobe`. All
metadata, conversion, trim, and export operations will fail with an opaque error if
these binaries are missing. The `yt-dlp` download similarly.
**Fix**: add a startup check (`exec.LookPath("ffmpeg")`) and log a prominent warning
if any required binary is missing.

---

## 6. UX Gaps

### 6a. No way to re-scan a single directory on demand
The user can add/remove directories, and the poller auto-scans every 60s, but there
is no "Rescan now" button for a specific directory. Useful after a bulk file copy.
**Fix**: add a `POST /directories/{id}/sync` endpoint triggered by a button in the
directory list row.

### 6b. Trim output silently overwrites `_trim` file if it already exists
`-y` flag is passed to ffmpeg, which overwrites without asking. The second trim of
the same video replaces the first silently.
**Fix**: append a counter suffix if the output file already exists, or show a
warning in the UI.

### 6c. Export USB destination is fixed to the source directory
The exported MP4 and the in-progress transcode both land in the source video's
directory. There is no way to choose a destination.
**Fix**: add an optional `destination` field to the export form, defaulting to the
source directory.

### 6d. Duplicate detection misses renamed copies
Same content, different filename = not detected. The current `(filename, size)` key
only finds verbatim copies placed in different directories.
**Fix**: for a more accurate (but slower) scan, hash the first 64KB of each file.
Keep the fast mode as-is for the "same file in two registered dirs" case.

### 6e. No bulk operations on videos
Rating, tagging, and deleting require acting on one video at a time. Power users
managing large imports want multi-select.
**Fix**: add checkboxes in the library sidebar with "Tag selected", "Delete selected",
"Rate selected" bulk actions.

### 6f. `#video-list` poll reloads mid-scroll
The 60s htmx poll replaces the entire `#video-list` content, resetting scroll
position. If the user is scrolled to the middle of a long list, the list jumps back
to the top.
**Fix**: use `hx-swap="morph"` (idiomorph extension) so htmx diffs the DOM instead
of replacing it, preserving scroll position and focus.

### 6g. yt-dlp download gives no progress feedback
The POST to `/ytdlp/download` runs yt-dlp synchronously and only returns when done.
Large downloads appear frozen. The spinner is shown, but there is no indication of
download speed or percentage.
**Fix**: stream yt-dlp output to the client via SSE or a polling job-status endpoint.

### 6h. Tab strip has no tooltip/title showing the full video name
When many tabs are open, the tab labels are truncated. Hovering shows the trimmed
text but no full-title tooltip.
**Fix**: add `title="..."` attribute to the `.tab-btn` element in the JS `openTab`
function.

### 6i. No way to jump to "next unwatched" video
In a series workflow, users want to open the next episode that doesn't have a
`watch_history` entry. There is no "next unwatched" button or sort mode.
**Fix**: add a "▶ Next unwatched" button per tag/directory that runs a query like
`SELECT v.* FROM videos v LEFT JOIN watch_history w ON v.id = w.video_id WHERE w.video_id IS NULL ORDER BY v.filename LIMIT 1`.

---

## 7. Testing Gaps

### 7a. No handler-level integration tests
All tests are in the `store` and `metadata` packages. The HTTP handlers in `main.go`
have zero test coverage. A handler test using `httptest.NewRecorder` would catch
regressions in routing, template rendering, and form parsing.

### 7b. No test for the JavaScript tab system
The multi-tab `openTab`/`activateTab`/`closeTab` logic is untested. A minimal
Playwright or `go test` + `chromedp` test would catch the OOB swap parsing and
script re-execution logic.

### 7c. Store interface not exercised against real SQLite in all paths
`GetRandomVideo` and `SearchVideos` have no dedicated test cases.

---

## 8. Minor Nits

- `io.ReadAll(resp.Body)` in `tmdbGet` discards error. Use `if body, err := io.ReadAll(...); err != nil { return err }`.
- `log.Fatal` on startup errors doesn't run deferred `store.Close()`. Use `log.Print` + `os.Exit(1)` after explicit cleanup, or check errors before calling `defer`.
- The `reltime` template function formats relative timestamps (e.g. "3d ago") but has no test for edge cases like future timestamps or zero values.
- `handleSaveSettings` validates `video_sort` to `"name"` or `"rating"`, but the sort is not actually applied in `serveVideoList` — `ListVideos` always sorts by display name, and `ListVideosByRating` is only called for ratings. The `video_sort` setting is read and stored but has no visible effect when filtering by tag.
- `tags.html` still has a handful of residual inline button styles that were not converted during the #18 styling cleanup.
