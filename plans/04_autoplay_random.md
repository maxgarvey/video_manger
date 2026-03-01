# Plan: Autoplay Random Episode on Start (Feature 4)

## Behaviour Change

When the page loads, instead of showing "Select a video to play",
automatically load a random video into the player.

## Implementation

### `main.go` — new route + handler

```
GET /play/random
```

- Lists all videos from the store.
- If none exist, renders a "No videos yet" placeholder into `#player`.
- Otherwise picks one at random using `math/rand`, then delegates to
  the same rendering logic as `handlePlayer`.

### `templates/index.html`

Change the `<main id="player">` element to trigger a load of
`/play/random` on page load:

```html
<main id="player"
      hx-get="/play/random"
      hx-trigger="load"
      hx-swap="innerHTML">
  <p style="color:#444">Loading…</p>
</main>
```

## Tests

- `TestHandleRandomPlayer_NoVideos` — 200, no `<video>` element.
- `TestHandleRandomPlayer_WithVideos` — 200, `<video>` element present.
