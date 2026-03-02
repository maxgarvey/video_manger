# Plan: yt-dlp download queue + queue progress UI

## Problem
Currently the UI supports one URL per submit and the new progress
output replaces the previous one. Users want to queue multiple URLs
and see progress for all of them.

## Approach — minimal, additive

### UI (index.html)
- Change `<input type="text" name="url">` to
  `<textarea name="urls" rows="3" placeholder="One URL per line">`.
- Change form to `hx-swap="beforeend"` so each submit *appends* a
  new progress block rather than replacing the old one.
- Add a small "Clear" button below the output div to let the user
  wipe completed/old job blocks from the list.

### Server (handlers.go)
- `handleYTDLPDownload`: read `r.FormValue("urls")`, split on
  newlines, trim, skip blanks. For each URL create an independent
  `ytdlpJob` and launch a goroutine exactly as today (semaphore
  limits concurrency automatically).
- Return HTML with one `ytdlp_progress.html`-equivalent block per
  URL (wrapped in a container div). The template now receives a slice
  of jobIDs rather than a single ID.
- Rename template to `ytdlp_queue_progress.html` (or add a range
  loop inside the existing template).

### Template changes
- `ytdlp_progress.html`: receive slice → range over jobs, each gets
  its own `<div>` with unique IDs derived from jobID.
- Each block shows the URL (truncated) as a label above the log.

## Concurrency
Jobs are submitted in parallel but the `convertSem` channel (capacity 2)
throttles actual yt-dlp processes, so excess jobs wait as before.

## Files changed
- `templates/index.html`: textarea, beforeend swap, clear button
- `handlers.go`: multi-URL split, slice of jobs
- `templates/ytdlp_progress.html`: accepts []jobEntry{JobID, URL}
