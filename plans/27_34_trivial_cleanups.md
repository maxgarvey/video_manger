# Plan: Trivial Cleanups (items 27–34)

All are pure cleanups — no behaviour changes, no new UI.

## Changes

### 27 — Remove dead `handleRandomPlayer` + `GET /play/random` route
- Delete route line `r.Get("/play/random", s.handleRandomPlayer)` from `routes()`
- Delete the `handleRandomPlayer` method body (lines 376-398)

### 28 — Remove discarded `strconv.Atoi(*port)`
- Delete line 109: `portInt, _ := strconv.Atoi(*port)`
- The variable `portInt` is the only argument passed to `zeroconf.Register`; need to
  inline the conversion with proper error handling, or keep the call with `strconv.Atoi`
  but handle the error.
  **Decision**: keep `Atoi` but log+continue on bad port string; `zeroconf.Register`
  is best-effort anyway. Actually simpler: keep the existing behavior but use `_` →
  change to a named variable with a logged error so the discard is intentional.
  Best pragmatic fix: just inline it as the argument and accept the zero value on
  parse failure — same as today but without the dead assignment.

### 29 — `DupGroup` → unexported `dupGroup`
- Rename struct and all usages within `main.go` and `duplicates.html`

### 30 — `handleDirectoryDeleteConfirm`: use `GetDirectory`
- Replace `ListDirectories` + loop with a single `s.store.GetDirectory(ctx, id)` call
- Return 404 on `sql.ErrNoRows`

### 31 — Tab strip: `title` tooltip on tab buttons
- In `openTab` JS in `index.html`, add `btn.title = title` when creating the tab button

### 32 — Settings: never echo TMDB key as input value
- In `settings.html`: change the key input to `type="password"` with no `value`
  attribute; show a "(already set)" hint when `TMDBKey != ""`

### 33 — Fix silently-discarded `io.ReadAll` error in `tmdbGet`
- Change `body, _ := io.ReadAll(resp.Body)` to check and return error

### 34 — Clean up residual inline button styles in `tags.html`
- Replace remaining `style="..."` on buttons with appropriate `btn-sm`/`btn-ghost` classes
