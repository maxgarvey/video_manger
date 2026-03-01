# Plan 18 — Consistent Button Styling

## Goal
Unify the ~16 ad-hoc inline button style patterns into a small set of CSS
classes, removing redundant inline styles.

## CSS classes (defined in index.html <style>)

| Class | Use |
|-------|-----|
| `button` (base) | All buttons: #2a2a2a bg, #3a3a3a border, #ccc text, hover transition |
| `.btn-sm` | Tight-layout variant: smaller padding + font |
| `.btn-danger` | Destructive: dark red bg/border, salmon text |
| `.btn-success` | Positive: dark green bg/border, mint text |
| `.btn-ghost` | Borderless, transparent — secondary/cancel |
| `.btn-icon` | Icon-only (✕, ↑): no bg/border, just the symbol |

## Template changes
- `index.html`: update CSS block; add class to ✕ close buttons
- `player.html`: remove redundant inline styles from action buttons; add `.btn-sm` to tag ✕
- `video_list.html`: `.btn-icon` on delete ✕
- `settings.html`: remove inline style from Save
- `video_delete_confirm.html`, `directory_delete_confirm.html`: `.btn-sm`, `.btn-danger`, `.btn-ghost`
- `share_panel.html`: `.btn-sm` on Copy
- `dir_browser.html`: `.btn-success`, `.btn-ghost`; `.btn-icon` on ↑
- `lookup_modal.html`, `lookup_results.html`: `.btn-success`
- `file_metadata.html`, `directories.html`: remove redundant inline
