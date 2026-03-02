# Plan: Fix "Loading…" placeholder on first load

## Problem
`index.html` shows `<p style="color:#444">Loading…</p>` inside `#tab-panes`
on startup. If there are no videos in the library, `fetch('/random-video')`
returns a non-OK response and `openTab` is never called, so "Loading…"
stays visible indefinitely.

## Fix
- Replace the hard-coded `<p>Loading…</p>` with an empty placeholder.
- In the auto-load JS, on failure (no random video), replace the placeholder
  with a friendly "Select a video from the library." message instead of
  leaving it blank or stuck on "Loading…".
