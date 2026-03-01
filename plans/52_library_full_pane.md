# Plan: Library as semi-transparent full-pane overlay (#52)

The library is currently a 300 px sidebar. Make it a full-screen
semi-transparent overlay so the paused video is visible behind it, and
use the extra space for a two-column layout with better video selection.

## CSS changes (`index.html`)

- `#library`: `inset: 0; width: auto` (full screen) instead of narrow
  sidebar; background `rgba(10,10,10,0.82)` + `backdrop-filter:blur(12px)`;
  change slide-in from translateX to fade-in (`opacity 0 → 1`).
- `#lib-btn`: remove the `left: 312px` slide-right rule (not needed
  when overlay is full-screen).
- Inner layout: two-column grid (`280px 1fr`).
  - Left column: existing directories, download, tags controls.
  - Right column: search input + video list (inherits scrolling).
- Video rows: slightly taller, more contrast on hover for easier clicking.
