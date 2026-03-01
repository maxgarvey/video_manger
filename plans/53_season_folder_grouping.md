# Plan: Season/folder grouping in library UI (#53)

Videos live in subdirectories (e.g. "Season 1", "Season 2"). Group
them visually in the video list so seasons/folders are collapsible
sections.

## Backend

- Add `videoGroup` struct `{Label string, Videos []store.Video}` in
  main.go (view layer only, no schema changes).
- `serveVideoList`: after fetching the flat list, for the default
  (non-search, non-tag) view sort by `DirectoryPath → Title` then
  group consecutive videos by `DirectoryPath`. Pass `Groups []videoGroup`
  to the template (alongside `History`).
- Tag-filter and search results: also grouped, but the directory sort
  re-order is skipped so ratings/search relevance is preserved within
  each group.

## Frontend (`video_list.html`)

- If `len(.Groups) == 1`: render exactly the current flat list (no header).
- If `len(.Groups) > 1`: wrap each group in `<details open>` with a
  `<summary>` showing the base directory name + video count badge.
  Each group's video list is the same row markup as today.
