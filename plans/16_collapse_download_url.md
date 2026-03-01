# Plan 16 — Collapse Download-from-URL Section

## Goal
Move the "Download from URL" section behind a click, matching the collapsible
`<details>` pattern already used by the Directories section.

## Changes
- `templates/index.html`: replace `<section>` / `<h2>` with `<details>` / `<summary>`
  for the yt-dlp download block.

## No backend changes needed.
