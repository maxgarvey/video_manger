# Review of Items 1–10 + New Items 11–14

## Items 1–10: Review Findings

### Feature 6 (video type indicators) — INCOMPLETE, uncommitted
- Migration, store, handler, filter logic are all good
- **Bug**: `video_type_badge.html` is never rendered anywhere in the UI
  - Missing from `video_list.html` rows (main ask)
  - Missing from `player.html` info panel
- **Bug**: `hx-include` on `#video-list` div does not include `#active-type`,
  so the type filter is lost on auto-refresh (every 60 s and on `videoRenamed`)

### Trim features 1–4 — partial / buggy

**Item #1 (draggable range bar) — INCOMPLETE**
- Only text inputs with `oninput` seek were added; the main feature
  (draggable visual region bar) was never implemented.
- The seek conversion is also wrong: `parseFloat(value.replace(/:/g,''))`
  turns "1:30" into `parseFloat("130") = 130` instead of 90 seconds.

**Item #2 + #9 conflict**
- When playing AND currentTime/duration > 0.7 both conditions fire:
  - start = currentTime  (item #2)
  - end   = currentTime  (item #9)
  giving start == end, which is an impossible / empty trim range.
- Fix: make the conditions mutually exclusive.
  - If playing → item #2 (stop, set start = currentTime, clear end)
  - Else if ≥70% → item #9 (set end = currentTime, leave start at 0)

### Items 7 & 8 (thumbnails) — OK
Thumbnail generation, storage, and serving look correct.

### Item 5 (show/season org) — OK

### Item 10 (quick label) — OK (improved to full-screen modal)

---

## Fix Plan (ordered by dependency / complexity)

| Step | What                                               | Files                                     |
|------|----------------------------------------------------|-------------------------------------------|
| F1   | Complete Feature 6: badge in list + player panel   | video_list.html, player.html, index.html  |
| F2   | Fix trim conflict (#2/#9) + seek conversion        | player.html, trim_panel.html              |
| F3   | Add draggable range bar to trim panel              | trim_panel.html                           |
| F4   | Fix streaming/seeking (item 11): exempt /video/{id}| server.go                                 |
| F5   | Video color controls (item 12)                     | player.html, handlers_videos.go           |
| F6   | Memory leak investigation (item 14)                | various                                   |

---

## New Items 11–14

### 11. Improve playback — seeking kills streaming session
`middleware.Compress(5)` is applied globally. For the `/video/{id}` route,
which uses `http.ServeFile`, this wraps the `http.ResponseWriter` with a
compression writer. Even if the compressor detects a non-compressible
content type, the wrapped writer can interfere with the 206 Partial Content
response that Range requests require for seeking.

**Fix**: Register `/video/{id}` in a sub-router that does NOT apply
`middleware.Compress`.  Add a `ReadHeaderTimeout` to the http.Server to
prevent idle connections from hanging.

### 12. Video color controls (tint/hue/saturation/brightness)
CSS `filter:` property can apply real-time adjustments to the `<video>`
element: `brightness()`, `contrast()`, `saturate()`, `hue-rotate()`,
`sepia()`.  Controls will be a collapsible panel below the video with range
sliders for each adjustment.  Settings stored in localStorage per-video.

### 13. Delete video from player — ALREADY DONE
`DELETE /videos/{id}` and `DELETE /videos/{id}/file` both call
`closeTab(videoID)` via `hx-on::after-request`.

### 14. Memory leaks — investigation
Candidates:
- `syncingDirs` map never cleaned up on error path
- SSE goroutines may leak if client disconnects before channel send
- `jobs` and `convertJobs` maps hold completed jobs indefinitely
- `sessions` map is pruned hourly — OK
