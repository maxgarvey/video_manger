# Code Review Issues

Generated from full codebase review. Working through these iteratively.

Legend: ⬜ Pending | 🔄 In Progress | ✅ Fixed | 📝 Documented (won't fix)

---

## Bugs / Correctness

| # | Issue | Status |
|---|-------|--------|
| B1 | `handleLookupApply`: metadata write failure is logged but user sees success UI | ⬜ |
| B2 | `handleConvert`: `UpsertVideo` error after conversion is silently ignored | ⬜ |
| B3 | `handleLookupApply` TV: series name fetch error is silently ignored; show name written as empty string | ⬜ |
| B4 | `handleLookupApply`: does not update `display_name` in DB after writing TMDB title to file | ⬜ |
| B5 | `handleConvert`: context cancel (user disconnect) kills ffmpeg and leaves a partial output file with no cleanup | ⬜ |
| B6 | `handleConvert`: converting to the same extension as the source (e.g. mkv→mkv) overwrites the input file | ⬜ |
| B7 | `SetVideoRating` / `handleSetRating`: non-existent video returns 500 instead of 404 | ✅ |
| B8 | `handleDeleteDirectoryAndFiles`: not transactional; mid-loop failure leaves DB and disk inconsistent | ⬜ |
| B9 | `handleYTDLPDownload`: timeout uses magic `1e9` literal instead of `time.Minute` | ✅ |
| B10 | `migration 001_initial.sql`: `PRAGMA foreign_keys = ON` runs inside a transaction where it is a no-op (FK enforcement comes only from `NewSQLite`) | 📝 |

---

## Safety / Data Loss

| # | Issue | Status |
|---|-------|--------|
| S1 | `handleExportUSB`: `_usb.mp4` file is left in source directory permanently; gets picked up by next sync | ⬜ |
| S2 | `tmdbGet`: uses `http.DefaultClient` with no timeout; hangs if TMDB is unresponsive | ⬜ |
| S3 | `syncDir`: outer `filepath.WalkDir` return value is silently ignored; directory-not-found produces no log | ✅ |
| S4 | Share panel "Copy" button uses `navigator.clipboard` which requires HTTPS; silently fails over HTTP on LAN | ⬜ |
| S5 | No CSRF protection on POST/DELETE/PUT routes | 📝 |

---

## Missing Test Coverage

| # | Issue | Status |
|---|-------|--------|
| T1 | `handleGetSettings` and `handleSaveSettings` have zero tests | ⬜ |
| T2 | `handleLookupModal` with API key set (search form branch) is untested | ⬜ |
| T3 | `handleLookupApply` with invalid `media_type` (→ 400) is untested | ⬜ |
| T4 | `handleRandomPlayer`: autoplay-disabled path, no-videos path, and normal path all untested | ⬜ |
| T5 | `handleDirectoryOptions` (`GET /directories/options`) is untested | ⬜ |
| T6 | `handleVideoList` with `?tag_id=` filter is untested | ⬜ |
| T7 | `handleVideoList` with `video_sort=rating` setting is untested | ⬜ |
| T8 | Store: `SetVideoRating`, `ListVideosByRating` untested at store layer | ⬜ |
| T9 | Store: `RecordWatch`, `GetWatch` untested at store layer | ⬜ |
| T10 | Store: `GetSetting`, `SetSetting` untested at store layer | ⬜ |
| T11 | Store: `GetDirectory` untested at store layer | ⬜ |
| T12 | Migration test does not verify `watch_history` table, `settings` table, or `rating` column existence | ⬜ |
| T13 | `metadata.Write` is completely untested | ⬜ |
| T14 | `handleLookupSearch` happy path (mocked TMDB response) is untested | ⬜ |

---

## Design Issues

| # | Issue | Status |
|---|-------|--------|
| D1 | Mixed raw SQL and sqlc-generated queries; `db/` package is partial and creates split maintenance | 📝 |
| D2 | `handleRandomPlayer` loads all videos into memory to pick one; should use `ORDER BY RANDOM() LIMIT 1` | ⬜ |
| D3 | No SQLite WAL mode or `SetMaxOpenConns(1)`; concurrent requests risk "database is locked" | ⬜ |
| D4 | `localAddresses` calls `net.Interfaces()` on every `/share` and `/info` request | 📝 |
| D5 | `SQLiteStore` has no `Close()` method; underlying `*sql.DB` never explicitly closed | ⬜ |
| D6 | Unused hidden `<div id="ytdlp-dirs">` in `index.html` fires a redundant `/directories` request on load | ⬜ |
| D7 | Unused tags accumulate forever; deleted videos leave orphan tag rows | ⬜ |
| D8 | Rating button markup is duplicated in `player.html` and `rating_buttons.html` | 📝 |
| D9 | `handleSaveSettings`: three `SetSetting` calls are not atomic; partial save possible on error | ⬜ |

---

## Behavioral Quirks

| # | Issue | Status |
|---|-------|--------|
| Q1 | `syncDir` tags all files with the *root* registered directory name, not the immediate subdirectory | 📝 |
| Q2 | `handleGetProgress`: "no watch" branch writes raw JSON string literal; "has watch" branch uses `json.Encoder` (adds trailing newline); inconsistent | ✅ |
| Q3 | Info panel shows nothing (no empty state) before a video is first selected | ⬜ |
| Q4 | `SearchVideos`: a query of `%` or `_` matches unexpected rows (SQL LIKE wildcards pass through) | ⬜ |
| Q5 | `video_list.html` tooltip (`title=`) always shows raw filename even when a display name is set | ⬜ |
| Q6 | `handleLookupSearch`: raw TMDB error message (potentially verbose) is forwarded directly to the client as 502 body | ⬜ |
