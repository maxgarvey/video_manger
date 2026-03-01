# Plan: Rescan directory button (#40)

Add a "↺ Rescan" button to each directory row that triggers an on-demand sync.

## Backend

New route: `POST /directories/{id}/sync`

Handler `handleSyncDirectory`: look up the directory by ID, call `syncDir`, then
re-render the directory list.

## Frontend

In `directories.html`, add a small "↺" button per row alongside the existing delete
button:

```html
<button class="btn-icon"
  hx-post="/directories/{{.ID}}/sync"
  hx-target="#dir-list"
  hx-swap="outerHTML"
  title="Rescan">↺</button>
```

The directory list partial already renders into `#dir-list` — it just needs that id.
