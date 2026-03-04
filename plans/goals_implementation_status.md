# Goals Implementation Status

Ordered by implementation priority (easiest/most foundational first).

| #   | Original # | Feature                              | Status  |
| --- | ---------- | ------------------------------------ | ------- |
| 1   | 8          | Recursive directory scan             | ✅ Done |
| 2   | 3          | Auto-tag videos with directory name  | ✅ Done |
| 3   | 2          | Formalize migrations directory       | ✅ Done |
| 4   | 4          | Autoplay random episode on start     | ✅ Done |
| 5   | 13         | Create new folders in-app            | ✅ Done |
| 6   | 6          | Track watched timestamps             | ✅ Done |
| 7   | 7          | Like and double-like                 | ✅ Done |
| 8   | 5          | Configuration menu drawer            | ✅ Done |
| 9   | 14         | Export to USB/BluRay format          | ✅ Done |
| 10  | 12         | yt-dlp import                        | ✅ Done |
| 11  | 9          | LAN sharing (show local IP)          | ✅ Done |
| 12  | 11         | mDNS `.local` hostname               | ✅ Done |
| 13  | 15         | Video format conversion              | ✅ Done |
| 14  | 1          | External metadata lookup (TMDB/TVDB) | ✅ Done |
| 15  | 10         | P2P sharing                          | ✅ Done |

## Round 2

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                                        | Status  |
| --- | ------- | -------------------------------------------------------------- | ------- |
| 16  | 2       | Collapse download-URL section behind click                     | ✅ Done |
| 17  | 10      | Show last-watched timestamp on video rows                      | ✅ Done |
| 18  | 5       | Consistent, minimal button styling across all templates        | ✅ Done |
| 19  | 7       | Better dropdown/details indicators (triangle, highlight state) | ✅ Done |
| 20  | 9       | Auto-update library as new files are added (polling)           | ✅ Done |
| 21  | 1       | Hover overlay on video player showing title / metadata         | ✅ Done |
| 22  | 11      | Detect duplicate files (by size+name hash)                     | ✅ Done |
| 23  | 3       | In-between states / animations (htmx indicators, transitions)  | ✅ Done |
| 24  | 4       | Library nav: smaller rows, more info, takes more screen        | ✅ Done |
| 25  | 6       | Video cropping via UI using ffmpeg                             | ✅ Done |
| 26  | 8       | Multiple video tabs (play several at once)                     | ✅ Done |

## Round 3 — Self-Review Action Items

Ordered by complexity (easiest / least invasive first).

| #   | Self-review ref | Feature                                                                           | Status  |
| --- | --------------- | --------------------------------------------------------------------------------- | ------- |
| 27  | dead code 3a    | Remove unused `handleRandomPlayer` + `GET /play/random` route                     | ✅ Done |
| 28  | dead code 3b    | Remove discarded `strconv.Atoi(*port)` result                                     | ✅ Done |
| 29  | quality 5c      | Make `DupGroup` unexported (`dupGroup`)                                           | ✅ Done |
| 30  | quality 5b      | `handleDirectoryDeleteConfirm`: use `GetDirectory` instead of linear scan         | ✅ Done |
| 31  | UX 6h           | Tab strip: add full-title `title` tooltip to tab buttons                          | ✅ Done |
| 32  | security 2c     | Settings: never echo TMDB API key back as input value                             | ✅ Done |
| 33  | nit             | Fix silently-discarded `io.ReadAll` error in `tmdbGet`                            | ✅ Done |
| 34  | nit             | Clean up residual inline button styles in `tags.html`                             | ✅ Done |
| 35  | arch 5f         | Startup check: warn if `ffmpeg`, `ffprobe`, or `yt-dlp` missing from PATH         | ✅ Done |
| 36  | perf 4a         | `serveVideoList`: replace two `watch_history` full scans with one                 | ✅ Done |
| 37  | quality 5d      | Deduplicate `handleAddDirectory` / `handleCreateDirectory` into shared helper     | ✅ Done |
| 38  | UX 6b           | Trim: append counter suffix instead of silently overwriting existing `_trim` file | ✅ Done |
| 39  | nit             | `video_sort` setting: actually route to `ListVideosByRating` when sort=rating     | ✅ Done |
| 40  | UX 6a           | Add "Rescan" button per directory (`POST /directories/{id}/sync`)                 | ✅ Done |
| 41  | UX 6f           | Use idiomorph `hx-ext="morph"` for video list swap to preserve scroll position    | ✅ Done |
| 42  | arch 5a         | Graceful shutdown: SIGTERM handler + cancel root context + `Server.Shutdown`      | ✅ Done |
| 43  | bug 1a          | `syncDir`: prune DB records for files deleted from disk                           | ✅ Done |
| 44  | bug 1c          | `handleConvert`: use background context so conversion survives browser disconnect | ✅ Done |
| 45  | bug 1b          | Multi-tab info panel: update panel when switching tabs                            | ✅ Done |
| 46  | security 2b     | `handleBrowseFS`: restrict path to home-dir subtree                               | ✅ Done |
| 47  | testing 7c      | Store tests for `GetRandomVideo` and `SearchVideos`                               | ✅ Done |
| 48  | UX 6i           | "Next unwatched" button per tag/directory                                         | ✅ Done |
| 49  | security 2a     | Optional password protection (bcrypt + cookie session)                            | ✅ Done |

## Round 4 — New Feature Requests

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                                 | Status  |
| --- | ------- | ------------------------------------------------------- | ------- |
| 50  | 4       | Add show name to hover overlay                          | ✅ Done |
| 51  | 5       | Video not found: offer delete or relocate               | ✅ Done |
| 52  | 2       | Library as semi-transparent full-pane overlay           | ✅ Done |
| 53  | 3       | Season/folder grouping in library UI                    | ✅ Done |
| 54  | 1       | Info panel always visible below video (scroll to reach) | ✅ Done |

## Round 5 — New Feature Requests

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                               | Status  |
| --- | ------- | ----------------------------------------------------- | ------- |
| 55  | 5       | Filter library by liked / superliked                  | ✅ Done |
| 56  | 3       | Show codec info (video/audio streams) via ffprobe     | ✅ Done |
| 57  | 6       | Manual "mark as watched" from info panel              | ✅ Done |
| 58  | 4       | Recursive directory scan with progress feedback       | ✅ Done |
| 59  | 2       | Configurable library location + copy files to library | ✅ Done |
| 60  | 1       | Drag and drop folders/files to import                 | ✅ Done |

## Round 6 — New TODOs

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                                            | Status  |
| --- | ------- | ------------------------------------------------------------------ | ------- |
| 61  | 4       | Fix CI / failing tests (format key rename + async convert handler) | ✅ Done |
| 62  | 2       | Don't display "Loading…" on first load when no videos available    | ✅ Done |
| 63  | 1       | Delete video from the player info panel                            | ✅ Done |
| 64  | 3       | Move video to a different directory (with optional new sub-folder) | ✅ Done |

## Round 7 — New TODOs

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                                             | Status  |
| --- | ------- | ------------------------------------------------------------------- | ------- |
| 65  | 1       | Close open tab when video deleted from library list                 | ✅ Done |
| 66  | 2       | Store original filename at import time                              | ✅ Done |
| 67  | 3       | Standardised fields: genre, season/episode, actors, studio, channel | ✅ Done |

## Round 8 — New TODOs

Ordered by complexity (easiest first).

| #   | TODOS # | Feature                                                                    | Status  |
| --- | ------- | -------------------------------------------------------------------------- | ------- |
| 68  | 3       | yt-dlp metadata tagging on import (write-info-json → ffmpeg stream-copy)   | ✅ Done |
| 69  | 1+2     | yt-dlp download queue + queue progress UI (textarea, beforeend, multi-job) | ✅ Done |

## Round 9 — Self-review fixes

Ordered by phase.

| #   | Ref   | Fix                                                                       | Status                                         |
| --- | ----- | ------------------------------------------------------------------------- | ---------------------------------------------- |
| 70  | 1a/5a | XSS: html.EscapeString in handleUpdateVideoName                           | ✅ Done                                        |
| 71  | 1b    | Cross-device move rollback + os.Remove error check                        | ✅ Done                                        |
| 72  | 5b    | Relocate: restrict newPath to registered directories                      | ✅ Done                                        |
| 73  | 1c    | yt-dlp [Merger] line preferred over [download] Destination                | ✅ Done                                        |
| 74  | 1d    | Original filename hint only shown when different from current             | ✅ Done                                        |
| 75  | 1f    | Log ListWatchHistory error instead of silently discarding                 | ✅ Done                                        |
| 76  | 1g    | metadata.Write after UpdateVideoFields                                    | ✅ Done                                        |
| 77  | 4a    | Auto-remove completed download blocks after 30s                           | ✅ Done                                        |
| 78  | 4c    | Stable video-fields container div ID                                      | ✅ Done                                        |
| 79  | 4e    | Clear URL textarea after submit (hx-on::after-request)                    | ✅ Done                                        |
| 80  | 4f    | Better "Connection lost" SSE copy                                         | ✅ Done                                        |
| 81  | 4g    | Disable quality inputs when mkv-copy selected                             | ✅ Done                                        |
| 82  | 4h    | Better actors placeholder ("e.g. Tom Hanks, Robin Wright")                | ✅ Done                                        |
| 83  | 1e/4b | HX-Trigger: videoRenamed refreshes library sidebar                        | ✅ Done                                        |
| 84  | 3a    | Tests: GET/GET-edit/PUT video fields + zero values                        | ✅ Done                                        |
| 85  | 3b    | Test: info.json read + cleanup path                                       | ✅ Done                                        |
| 86  | 3f    | Test: HTML-escaped title in rename response + HX-Trigger header           | ✅ Done                                        |
| 87  | 3e    | Tests: pagination page=1/3/10 with limit=2                                | ✅ Done                                        |
| 88  | 2b    | SQL LEFT JOIN watch_history in all list queries (remove ListWatchHistory) | ✅ Done                                        |
| 89  | 2c    | SQL ORDER BY directory_path (remove Go-level slices.SortFunc)             | ✅ Done                                        |
| 90  | 2a    | SQL LIMIT/OFFSET pagination                                               | ⏸ Deferred (requires store interface refactor) |

## Round 4 — New TODOS Features

Ordered by ease of implementation (simplest first).

| #   | TODOS # | Feature                                                                                                              | Status         |
| --- | ------- | -------------------------------------------------------------------------------------------------------------------- | -------------- |
| 91  | 3       | Add clear labeling during trim mode that you're selecting the region to keep                                         | ✅ Done        |
| 92  | 4       | Make a confirmation after the user has highlighted a region or entered video times                                   | ✅ Done        |
| 93  | 9       | If video played to 70% or more, assume delete end by selecting front region to keep when trim clicked                | ✅ Done        |
| 94  | 2       | If crop button hit while playing, stop playback and highlight region from current spot to end                        | ✅ Done        |
| 95  | 1       | Improve trim UI with draggable bar underneath progress bar for region to keep, fine-tune with inputs, seek on update | 🔄 In Progress |
| 96  | 10      | Add "Quick label" button to info pane with modal for Title, Movie/TV, Season, Episode, Genre, etc.                   | ⏳ Planned     |
| 97  | 7       | Support thumbnails in the UI for videos                                                                              | ⏳ Planned     |
| 98  | 8       | Automatically generate thumbnail image from video, with regenerate button                                            | ⏳ Planned     |
| 99  | 5       | Organize season and episodes under specific shows                                                                    | ⏳ Planned     |
| 100 | 6       | Add UI indicators on videos for type (TV show, movie, concert, vlog, blog, YouTube)                                  | ⏳ Planned     |
