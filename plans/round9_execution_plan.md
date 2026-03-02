# Round 9 — Execution Plan

Fixes from `self_review_round9.md` grouped into 6 phases.
Each phase is committed separately. Phases are ordered so the most
impactful / highest-risk items land first.

---

## Phase 1 — Security & high-severity bugs (all easy/medium)
Items: 1a, 1b, 5b

| Item | File(s) | Change |
|------|---------|--------|
| 1a/5a | handlers.go | `html.EscapeString(video.Title())` in handleUpdateVideoName |
| 1b | handlers.go | Check os.Remove error; if UpdateVideoPath fails, move file back |
| 5b | handlers.go | Restrict handleRelocateVideo newPath to registered directories |

---

## Phase 2 — Medium bugs & easy one-liners
Items: 1c, 1d, 1f, 1g, 1h

| Item | File(s) | Change |
|------|---------|--------|
| 1c | handlers.go | Watch `[Merger] Merging formats into` line; prefer over Destination |
| 1d | templates/player.html | Condition: ne OriginalFilename Filename |
| 1f | handlers.go | Log ListWatchHistory error instead of discarding |
| 1g | handlers.go | Call metadata.Write after UpdateVideoFields |
| 1h | handlers.go | Return error + attempt file rollback if UpdateVideoPath fails in move |

---

## Phase 3 — UX easy wins
Items: 4a, 4c, 4e, 4f, 4g, 4h

| Item | File(s) | Change |
|------|---------|--------|
| 4a | templates/ytdlp_progress.html + index.html | Auto-remove completed blocks after 30s |
| 4c | templates/player.html | Give fields div stable ID; use hx-target instead of outerHTML |
| 4e | templates/index.html | `hx-on::after-request="this.reset()"` on download form |
| 4f | templates/ytdlp_progress.html | Update "Connection lost" copy |
| 4g | templates/player.html | Disable quality radios in JS when mkv-copy selected |
| 4h | templates/video_fields_edit.html | Better actors placeholder |

---

## Phase 4 — Rename refreshes sidebar (medium)
Items: 1e, 4b

| Item | File(s) | Change |
|------|---------|--------|
| 1e/4b | handlers.go, templates/player.html | Return OOB video-list swap from handleUpdateVideoName |

---

## Phase 5 — Test coverage
Items: 3a, 3b, 3c, 3d, 3e, 3f

| Item | File(s) | Change |
|------|---------|--------|
| 3a | main_test.go | Tests for GET/GET-edit/PUT video fields |
| 3b | main_test.go | yt-dlp info.json path capture + metadata write test |
| 3c | main_test.go | handleExportUSB tests |
| 3d | main_test.go | handleListDuplicates handler test |
| 3e | main_test.go | Pagination tests for serveVideoList |
| 3f | main_test.go | Rename response body assertion (HTML-escaped) |

---

## Phase 6 — Performance
Items: 2b, 2c, 2a (2d and 2e deferred — low priority / hard)

| Item | File(s) | Change |
|------|---------|--------|
| 2b | store/sqlite.go, handlers.go | JOIN watch_history in video list queries; remove ListWatchHistory from serveVideoList |
| 2c | handlers.go | Remove Go-side slices.SortFunc; rely on SQL ORDER BY |
| 2a | store/sqlite.go, store/store.go, handlers.go | Add LIMIT/OFFSET to all list queries; pass page/limit into store layer |

---

## Status tracking

| Phase | Status |
|-------|--------|
| 1 — Security & high bugs | ⬜ Pending |
| 2 — Medium bugs | ⬜ Pending |
| 3 — UX easy wins | ⬜ Pending |
| 4 — Rename sidebar refresh | ⬜ Pending |
| 5 — Tests | ⬜ Pending |
| 6 — Performance | ⬜ Pending |
