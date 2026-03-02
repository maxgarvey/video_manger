# Plan: Close tab when a video is deleted from the library list

## Problem
Deleting a video via the ✕ button in the library list removes it from the DB
and refreshes #video-list, but any open player tab for that video stays open
with a broken/stale player.

## Fix
Add `hx-on::after-request="closeTab({{.ID}})"` to both delete buttons in
`video_delete_confirm.html` (the inline confirm fragment shown in the list).
`closeTab` is already a global function defined in index.html.
