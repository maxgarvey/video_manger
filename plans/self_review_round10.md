# Self-Review — Round 10: Readability, Maintainability & UX

Conducted after Round 9 fixes. Lens: code quality, maintainability, ease of use,
and user experience — NOT bugs or performance (those were Round 9).

Items rated **(low/medium/high)** by impact.

---

## 1 — CODE READABILITY

### 1a — handlers.go is a 1,900+ line monolith (HIGH)
`handlers.go` covers video CRUD, directories, metadata, file I/O, yt-dlp, TMDB,
auth, sessions, settings, P2P, duplicates, trimming, export — all in one file with
no section separators. Functions are interleaved with no grouping.
**Fix:** Split into focused files: `handlers_videos.go`, `handlers_directories.go`,
`handlers_conversion.go`, `handlers_metadata.go`, `handlers_auth.go`. **(high effort)**

### 1b — 30+ identical `parseIDParam` + `GetVideo` boilerplate blocks (MEDIUM)
Every handler that works on a specific video repeats the same 8-line pattern:
parse ID → check ok → GetVideo → check err → return 404. Nothing abstracts it.
**Fix:** `func (s *server) videoOrError(w, r) (store.Video, bool)` helper. **(easy)**

### 1c — Inconsistent error logging levels (MEDIUM)
`metadata.Write` failure is `slog.Warn` in `handleUpdateVideoName` (line 148)
but `slog.Error` in `handleLookupApply` (line 1597). Same severity, different level.
`os.Remove` failure after a successful copy is `slog.Warn` but a stale source file
is actually notable. No documented policy for Warn vs Error.
**Fix:** Establish convention: Error = user operation failed, Warn = degraded but
continuing, Info = normal events. Apply consistently. **(easy)**

### 1d — S1/S4/R8 comment tags unexplained (LOW)
Lines like `// S4: cap the body…` (line 650) and `// S1: strip directory…` (line 662)
use unexplained prefixes. Appear to be reviewer tags from an earlier draft.
**Fix:** Remove or replace with plain English comments. **(trivial)**

### 1e — `strPtr` helper is cryptic at call sites (LOW)
`strPtr("title")` reads like it returns a pointer to the string "title", but it
actually returns a pointer to `r.FormValue("title")`. The name hides the real
operation.
**Fix:** Inline it or rename: `formPtr(r, "title")`. **(easy)**

---

## 2 — CODE ORGANIZATION

### 2a — No grouping or section headings in handlers.go (HIGH)
1,900 lines with no `// --- Section ---` headers. Finding `handleExportUSB` requires
grep; there's no orientation.
**Fix:** At minimum, add section comment headers today. Splitting into files is the
right long-term fix (see 1a). **(trivial/high effort)**

### 2b — Repeated job-lifecycle boilerplate in conversion and yt-dlp (MEDIUM)
`handleYTDLPDownload` and `handleConvertStart` each:
1. Generate a UUID job ID
2. Create a job struct
3. Store in a mutex-protected map
4. Spawn a goroutine with identical cleanup pattern
5. Schedule a 10-minute deletion via `time.AfterFunc`

Both do this with ~30 lines of identical code.
**Fix:** Extract a `launchJob[T any](s, newT) (jobID, *T)` generic helper or at
least a `registerJob` + `cleanupJob` pair. **(medium)**

### 2c — SSE event sending duplicated in three places (MEDIUM)
`send(line)` closures in yt-dlp and conversion goroutines, plus `sseWrite` in
`handleYTDLPEvents` / `handleConvertEvents`, all manually format `data: …\n\n`.
**Fix:** Extract `type sseWriter struct{ w http.ResponseWriter }` with `Send(msg)`
and `SendEvent(event, data)` methods. **(easy)**

### 2d — Template data structs are all anonymous (MEDIUM)
Every `render(w, "foo.html", struct{ ... }{...})` uses an inline anonymous struct.
Refactoring a template means hunting down the render call in a 1,900-line file.
**Fix:** Define named types, e.g. `type playerData struct { ... }`. Even just for
templates that are rendered in more than one place. **(easy)**

### 2e — UUID / token generation repeated in three handlers (LOW)
Lines 116-121, 771-776, 954-959 all do the same `crypto/rand` hex-encode dance.
**Fix:** Extract `newToken() string` helper. **(trivial)**

---

## 3 — MAINTAINABILITY

### 3a — Magic numbers scattered throughout handlers.go (HIGH)

| Line | Value | Meaning |
|------|-------|---------|
| 379 | `500` | Default page size |
| 650 | `8 << 30` | Max upload bytes (8 GB) |
| 651 | `64 << 20` | Multipart form buffer |
| 1432 | `15 * time.Second` | TMDB HTTP timeout |
| 1495 | `10` | TMDB search result limit |

All should be named constants at the top of the relevant file. **(easy)**

### 3b — Inline styles make UI changes expensive (HIGH)
`index.html` has 280 lines of `<style>` block **plus** 500+ inline `style=`
attributes. The color `#2a2a2a` appears 20+ times inline; `#444` 15+ times.
Changing the dark theme means a search-and-replace across hundreds of attributes.
**Fix:** Define CSS custom properties (`--color-surface: #2a2a2a`) and use class
names. Even a minimal `--color-bg`, `--color-border`, `--color-text` cuts 80% of
inline repetition. **(medium)**

### 3c — Repeated input/button style strings (MEDIUM)
The 60-character inline style for a dark text input
(`background:#222;border:1px solid #444;color:#eee;padding:...;border-radius:4px`)
appears ~12 times across templates word-for-word. A typo in one is invisible.
**Fix:** `.input-dark` CSS class covers all of them. **(easy)**

### 3d — `handleImportUpload` mixes HTTP, file I/O, and DB in 65 lines (MEDIUM)
The function validates the multipart form, writes the file, registers it in the DB,
and renders a response — with no separation. A test can't exercise file writing
without a full HTTP stack.
**Fix:** Extract `func importFile(ctx, store, dir, filename, reader) error` business
logic from the HTTP handler. **(medium)**

### 3e — Path safety check duplicated across handlers (MEDIUM)
`handleBrowseFS` (line 1768) and `handleRelocateVideo` (line 302) each independently
implement "path must be under home dir or registered dir" logic with slightly
different approaches. One drift could introduce a path traversal gap.
**Fix:** Extract `func isAllowedPath(dirs []store.Directory, p string) bool` used by both. **(easy)**

### 3f — No config-file support; all settings via CLI flags (LOW)
`-db`, `-port`, `-password`, `-dir` are all flag-only. Longer config (multiple dirs,
TLS cert path, log level) would require a very long command.
**Fix:** Support an optional `config.toml` / `.env` file as an alternative.
**(medium effort, low urgency)**

---

## 4 — USER EXPERIENCE

### 4a — Chrome buttons invisible on first load (HIGH)
The Library, Info, and Settings buttons (top row) have `opacity: 0` and only
reveal on `:hover`. On touch devices there is no hover — these buttons are
completely undiscoverable. A new user on a tablet literally cannot open the library.
**Fix:** Set minimum `opacity: 0.35` (visible but subtle) so buttons always exist
visually. **(trivial)**

### 4b — Empty library gives no actionable guidance (HIGH)
"No videos found. Add a directory to get started." is shown but the library panel
that contains the "Add content" controls is collapsed by default. A new user reads
the message and doesn't know where to click.
**Fix:** When library is empty **and** library panel is closed, show a prominent
"Open library →" CTA or auto-open the library panel. **(easy)**

### 4c — No onboarding for required binaries (MEDIUM)
If ffmpeg/ffprobe/yt-dlp are missing, features fail silently (500 on convert, 500
on export, nothing on download). The startup warning goes to stderr which the user
never sees in a browser.
**Fix:** When a feature requiring a binary is triggered and the binary is missing,
return a user-readable message: "ffmpeg is required for conversion. Install it via
`brew install ffmpeg`." **(easy)**

### 4d — Metadata write failures shown as success to user (MEDIUM)
`handleUpdateVideoName` and `handleUpdateVideoFields` log metadata write errors
as warnings but return HTTP 200. The user sees "✓ Renamed" and has no idea the
file's embedded title is out of sync.
**Fix:** Treat metadata write failure as a soft warning in the response body:
render the success state with an additional `<p class="warn">Note: could not update
file metadata — file may be read-only.</p>`. **(easy)**

### 4e — Long file operations (move, copy, import) have no progress indicator (MEDIUM)
Moving a 20 GB video to a different drive, copying to library, or uploading a large
file over LAN gives the user no feedback. The button goes into htmx-request state
(slightly dimmed) but nothing more.
**Fix:** For move and copy-to-library: show a spinner + "Moving…" status that
replaces the button. For upload: show a `<progress>` element via `XMLHttpRequest`
or fetch with progress event. **(medium)**

### 4f — Settings success has no confirmation message (LOW)
Saving settings re-renders the settings form identically. The user has no way to
know if the save succeeded other than the panel not showing an error.
**Fix:** Flash a "Saved ✓" inline message for 2 seconds after save. **(easy)**

### 4g — TMDB key input is `type="password"` but hard to copy/paste (LOW)
The TMDB key is masked which prevents accidental screen-sharing leaks, but also
makes it impossible to verify. Users often paste a key, see 32 dots, and can't
confirm it's right.
**Fix:** Add a show/hide toggle eye icon on the TMDB key input. **(easy)**

### 4h — Keyboard shortcuts are invisible (LOW)
`L` opens library, `I` opens info, `S` opens settings, `Esc` closes. These are
defined in code (index.html ~line 742) but nowhere documented in the UI.
**Fix:** Show a small "?" help icon that on hover/click shows a shortcut cheat-sheet.
Or add `title` tooltips to each chrome button listing the shortcut. **(easy)**

### 4i — Tab strip has no scrollbar affordance on mobile (LOW)
The tab strip uses `overflow-x: auto` but doesn't reveal a scrollbar on iOS.
Multiple open tabs overflow silently.
**Fix:** Add a fade gradient on the right edge of the tab strip when tabs overflow,
signalling that more tabs exist. **(easy)**

### 4j — Confirmation dialog for convert/trim missing (LOW)
Delete uses `hx-confirm`. Convert and Trim are irreversible (they write output
files) but have no confirmation. A mis-click starts a long ffmpeg job.
**Fix:** Show a small inline "Are you sure? [Convert] [Cancel]" state before
submitting the convert form. **(easy)**

### 4k — "Connection lost" for SSE shows even during normal navigation (LOW)
If the user navigates away from the page while a download is running, `es.onerror`
fires and "Connection lost" is stored in the now-removed DOM. When they return the
block is gone. But for tabs that stay open, the message is jarring.
**Fix:** Distinguish `readyState === EventSource.CLOSED` (intentional) from
unexpected closure. Only show the message on unexpected closure. **(easy)**

---

## 5 — ACCESSIBILITY

### 5a — Invisible chrome buttons not keyboard-reachable (HIGH)
With `opacity: 0`, the Library/Info/Settings buttons have no visual indicator of
focus — a keyboard user pressing Tab would land on them but see nothing. Combined
with no `aria-label`, screen readers get nothing useful.
**Fix:** Same as 4a (min opacity) plus `aria-label="Open Library (L)"` etc. **(trivial)**

### 5b — No `<label>` elements on login form (MEDIUM)
`login.html` uses `<input placeholder="Password">` with no `<label>`. Screen
readers announce only "Password, text input" with no form context.
**Fix:** Add visible or visually-hidden `<label for="password">`. **(trivial)**

### 5c — Icon-only buttons have no accessible name (MEDIUM)
Many buttons are icon/emoji only: `✕` (close tab), `☰` (library), `⊟` (crop).
No `aria-label` or `title` attribute.
**Fix:** Add `aria-label` and/or `title` to every icon button. **(easy)**

### 5d — No focus management when panels open (LOW)
When the library panel opens, focus stays on the trigger button. A keyboard user
must Tab through the entire page to reach the library content.
**Fix:** On panel open, `focus()` the first interactive element inside the panel. **(easy)**

---

## 6 — DEVELOPER EXPERIENCE

### 6a — No package-level or file-level documentation (MEDIUM)
`handlers.go`, `server.go`, `main.go` have no `// Package main …` comment or
file-level doc explaining what the file contains. A contributor reading the code
for the first time has no map.
**Fix:** Add 2-3 sentence file header comments. **(trivial)**

### 6b — Template rendering errors are silent at startup (MEDIUM)
`template.Must(template.ParseGlob(...))` panics at startup if a template fails
to parse — this is fine. But adding a new template file with a syntax error gives
a panic with a stack trace rather than a helpful "template foo.html: line 12: …"
message.
**Fix:** `template.ParseGlob` already returns a descriptive error; `template.Must`
propagates the message in the panic. This is actually OK. But consider logging the
template name on render failure in `render()` function. **(easy)**

### 6c — No way to disable auth in tests without workarounds (LOW)
`newTestServer(t)` creates a server with `sessions: make(map[string]time.Time)` but
the auth middleware checks `s.password`. Tests that need auth must manually set the
password field and manage cookies.
**Fix:** Add test helper `newTestServerNoAuth(t)` or `WithNoAuth()` option. **(easy)**

---

## Summary table

| # | Category | Impact | Effort | Item |
|---|----------|--------|--------|------|
| 1a | Readability | HIGH | HIGH | Split handlers.go |
| 1b | Readability | MEDIUM | EASY | videoOrError helper |
| 1c | Readability | MEDIUM | EASY | Consistent log levels |
| 1d | Readability | LOW | TRIVIAL | Remove S1/S4 comment tags |
| 1e | Readability | LOW | EASY | Rename strPtr |
| 2a | Organization | HIGH | TRIVIAL/HIGH | Section headers in handlers |
| 2b | Organization | MEDIUM | MEDIUM | Job lifecycle helper |
| 2c | Organization | MEDIUM | EASY | sseWriter helper |
| 2d | Organization | MEDIUM | EASY | Named template data structs |
| 2e | Organization | LOW | TRIVIAL | newToken() helper |
| 3a | Maintainability | HIGH | EASY | Named constants |
| 3b | Maintainability | HIGH | MEDIUM | CSS custom properties |
| 3c | Maintainability | MEDIUM | EASY | .input-dark CSS class |
| 3d | Maintainability | MEDIUM | MEDIUM | Extract handleImportUpload logic |
| 3e | Maintainability | MEDIUM | EASY | isAllowedPath helper |
| 3f | Maintainability | LOW | MEDIUM | Config file support |
| 4a | UX | HIGH | TRIVIAL | Chrome buttons always visible |
| 4b | UX | HIGH | EASY | Empty state CTA |
| 4c | UX | MEDIUM | EASY | Binary-missing user messages |
| 4d | UX | MEDIUM | EASY | Soft warn on metadata failure |
| 4e | UX | MEDIUM | MEDIUM | Progress for move/copy/upload |
| 4f | UX | LOW | EASY | Settings saved confirmation |
| 4g | UX | LOW | EASY | TMDB key show/hide toggle |
| 4h | UX | LOW | EASY | Keyboard shortcut cheat-sheet |
| 4i | UX | LOW | EASY | Tab overflow gradient |
| 4j | UX | LOW | EASY | Confirm before convert/trim |
| 4k | UX | LOW | EASY | SSE close distinction |
| 5a | Accessibility | HIGH | TRIVIAL | Chrome buttons aria-label + opacity |
| 5b | Accessibility | MEDIUM | TRIVIAL | Login form labels |
| 5c | Accessibility | MEDIUM | EASY | Icon button aria-labels |
| 5d | Accessibility | LOW | EASY | Focus management on panel open |
| 6a | DX | MEDIUM | TRIVIAL | File header comments |
| 6b | DX | MEDIUM | EASY | Template render error logging |
| 6c | DX | LOW | EASY | Test auth helper |
