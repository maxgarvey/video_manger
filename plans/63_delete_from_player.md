# Plan: Delete video from player info panel

## Problem
The only way to delete a video is the small ✕ button in the library list.
There is no delete affordance in the info panel shown while watching.

## Fix
- Add a "✕ Delete" button to the info-panel watch controls row in `player.html`.
- Use `hx-get="/videos/{id}/delete-confirm"` targeting a new `#delete-modal-{id}`
  div sitting just below — the same HTMX confirm/delete pattern already used in
  the video list.
- On confirm, `hx-delete` already returns the updated video list HTML (hx-target
  `#video-list`). Add `hx-on::after-request="closeTab(id)"` so the tab also closes.
