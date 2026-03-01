# Goals Implementation Status

Ordered by implementation priority (easiest/most foundational first).

| # | Original # | Feature | Status |
|---|-----------|---------|--------|
| 1 | 8 | Recursive directory scan | ✅ Done |
| 2 | 3 | Auto-tag videos with directory name | ✅ Done |
| 3 | 2 | Formalize migrations directory | ✅ Done |
| 4 | 4 | Autoplay random episode on start | ✅ Done |
| 5 | 13 | Create new folders in-app | ✅ Done |
| 6 | 6 | Track watched timestamps | ✅ Done |
| 7 | 7 | Like and double-like | ✅ Done |
| 8 | 5 | Configuration menu drawer | ✅ Done |
| 9 | 14 | Export to USB/BluRay format | ✅ Done |
| 10 | 12 | yt-dlp import | ✅ Done |
| 11 | 9 | LAN sharing (show local IP) | ✅ Done |
| 12 | 11 | mDNS `.local` hostname | ✅ Done |
| 13 | 15 | Video format conversion | ✅ Done |
| 14 | 1 | External metadata lookup (TMDB/TVDB) | ✅ Done |
| 15 | 10 | P2P sharing | ✅ Done |

## Round 2

Ordered by complexity (easiest first).

| # | TODOS # | Feature | Status |
|---|---------|---------|--------|
| 16 | 2 | Collapse download-URL section behind click | ✅ Done |
| 17 | 10 | Show last-watched timestamp on video rows | ✅ Done |
| 18 | 5 | Consistent, minimal button styling across all templates | ✅ Done |
| 19 | 7 | Better dropdown/details indicators (triangle, highlight state) | ✅ Done |
| 20 | 9 | Auto-update library as new files are added (polling) | ✅ Done |
| 21 | 1 | Hover overlay on video player showing title / metadata | ✅ Done |
| 22 | 11 | Detect duplicate files (by size+name hash) | ✅ Done |
| 23 | 3 | In-between states / animations (htmx indicators, transitions) | ✅ Done |
| 24 | 4 | Library nav: smaller rows, more info, takes more screen | ✅ Done |
| 25 | 6 | Video cropping via UI using ffmpeg | ✅ Done |
| 26 | 8 | Multiple video tabs (play several at once) | ✅ Done |

## Round 3 — Self-Review Action Items

Ordered by complexity (easiest / least invasive first).

| # | Self-review ref | Feature | Status |
|---|-----------------|---------|--------|
| 27 | dead code 3a | Remove unused `handleRandomPlayer` + `GET /play/random` route | ✅ Done |
| 28 | dead code 3b | Remove discarded `strconv.Atoi(*port)` result | ✅ Done |
| 29 | quality 5c | Make `DupGroup` unexported (`dupGroup`) | ✅ Done |
| 30 | quality 5b | `handleDirectoryDeleteConfirm`: use `GetDirectory` instead of linear scan | ✅ Done |
| 31 | UX 6h | Tab strip: add full-title `title` tooltip to tab buttons | ✅ Done |
| 32 | security 2c | Settings: never echo TMDB API key back as input value | ✅ Done |
| 33 | nit | Fix silently-discarded `io.ReadAll` error in `tmdbGet` | ✅ Done |
| 34 | nit | Clean up residual inline button styles in `tags.html` | ✅ Done |
| 35 | arch 5f | Startup check: warn if `ffmpeg`, `ffprobe`, or `yt-dlp` missing from PATH | ✅ Done |
| 36 | perf 4a | `serveVideoList`: replace two `watch_history` full scans with one | ✅ Done |
| 37 | quality 5d | Deduplicate `handleAddDirectory` / `handleCreateDirectory` into shared helper | ✅ Done |
| 38 | UX 6b | Trim: append counter suffix instead of silently overwriting existing `_trim` file | ✅ Done |
| 39 | nit | `video_sort` setting: actually route to `ListVideosByRating` when sort=rating | ✅ Done |
| 40 | UX 6a | Add "Rescan" button per directory (`POST /directories/{id}/sync`) | ✅ Done |
| 41 | UX 6f | Use idiomorph `hx-ext="morph"` for video list swap to preserve scroll position | ✅ Done |
| 42 | arch 5a | Graceful shutdown: SIGTERM handler + cancel root context + `Server.Shutdown` | ✅ Done |
| 43 | bug 1a | `syncDir`: prune DB records for files deleted from disk | ✅ Done |
| 44 | bug 1c | `handleConvert`: use background context so conversion survives browser disconnect | ✅ Done |
| 45 | bug 1b | Multi-tab info panel: update panel when switching tabs | ✅ Done |
| 46 | security 2b | `handleBrowseFS`: restrict path to home-dir subtree | ✅ Done |
| 47 | testing 7c | Store tests for `GetRandomVideo` and `SearchVideos` | ✅ Done |
| 48 | UX 6i | "Next unwatched" button per tag/directory | ✅ Done |
| 49 | security 2a | Optional password protection (bcrypt + cookie session) | ✅ Done |
