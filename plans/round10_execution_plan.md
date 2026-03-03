# Round 10 — Execution Plan

Fixes from `self_review_round10.md` grouped into 5 phases.
Each phase committed separately. Ordered highest-value / lowest-effort first.

---

## Phase 1 — Trivial wins (easy/trivial items across all categories)
Items: 1d, 2a, 2e, 3a, 4a/5a, 5b, 5c, 6a

| Item | File(s) | Change |
|------|---------|--------|
| 1d | handlers.go | Remove S1/S4/R8 comment tags |
| 2a | handlers.go | Add `// --- Section ---` headings throughout |
| 2e | handlers.go | Extract `newToken() string` helper |
| 3a | handlers.go | Named constants for magic numbers |
| 4a/5a | templates/index.html | Chrome buttons min-opacity + aria-label |
| 5b | templates/login.html | Add `<label>` elements to login form |
| 5c | templates/index.html, player.html | aria-label on icon-only buttons |
| 6a | handlers.go, server.go, main.go | File header comments |

---

## Phase 2 — Code quality helpers (easy)
Items: 1b, 1c, 1e, 2c, 2d, 3e

| Item | File(s) | Change |
|------|---------|--------|
| 1b | handlers.go | `videoOrError` helper; use in all video handlers |
| 1c | handlers.go | Normalise slog levels (Warn=degraded, Error=failed) |
| 1e | handlers.go | Rename `strPtr` → `formPtr(r, key)` |
| 2c | handlers.go | `sseWriter` helper type for SSE formatting |
| 2d | handlers.go | Named structs for template data |
| 3e | handlers.go | Extract `isAllowedPath(dirs, path) bool` helper |

---

## Phase 3 — CSS / template maintainability (medium)
Items: 3b, 3c, 4f, 4g, 4h, 4i, 4j

| Item | File(s) | Change |
|------|---------|--------|
| 3b | templates/index.html | CSS custom properties for colors |
| 3c | templates/index.html + others | `.input-dark`, `.flex-col` etc. utility classes |
| 4f | templates/settings.html | Flash "Saved ✓" after settings save |
| 4g | templates/settings.html | TMDB key show/hide toggle |
| 4h | templates/index.html | Keyboard shortcut tooltip on chrome buttons |
| 4i | templates/index.html | Tab-strip overflow gradient |
| 4j | templates/player.html | Inline confirm before convert/trim |

---

## Phase 4 — UX improvements (easy/medium)
Items: 4b, 4c, 4d, 4e, 4k, 5d, 6b, 6c

| Item | File(s) | Change |
|------|---------|--------|
| 4b | templates/index.html | Empty state CTA opens library panel |
| 4c | handlers.go | User-friendly message when binary missing |
| 4d | handlers.go | Soft warn in response on metadata write failure |
| 4e | templates/index.html, handlers.go | Progress indicator for move/copy |
| 4k | templates/ytdlp_progress.html | SSE close: distinguish intentional vs unexpected |
| 5d | templates/index.html | Focus management when panels open |
| 6b | main.go | Log template name on render failure |
| 6c | main_test.go | Test auth helper |

---

## Phase 5 — Split handlers.go (high effort)
Items: 1a, 2b, 3d

| Item | File(s) | Change |
|------|---------|--------|
| 2b | handlers.go | Extract job lifecycle helper before split |
| 3d | handlers.go | Extract `importFile` business logic |
| 1a | handlers.go → 5 files | Split into handlers_{videos,directories,conversion,metadata,auth}.go |

---

## Status tracking

| Phase | Status |
|-------|--------|
| 1 — Trivial wins | ✅ Done |
| 2 — Code quality helpers | ✅ Done |
| 3 — CSS/template maintainability | ✅ Done |
| 4 — UX improvements | ✅ Done |
| 5 — Split handlers.go | ✅ Done |
