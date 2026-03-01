# Plan: Info panel always visible below video (scroll to reach) (#54)

## Current layout (constraints)
- `body { overflow: hidden; height: 100vh }` — no scroll
- `#player` is `position: fixed; inset: 0` — full viewport
- `#info-panel` is `position: fixed; bottom: 0` — slides up as overlay
- `#tab-panes` is `position: absolute; inset: 0`

## Target layout

The page scrolls vertically. The video area is exactly `100vh`, and
the info panel is a normal block below it (reached by scrolling down).
The library, config, and tab-strip overlays are unchanged.

Key changes:
1. `body`: remove `overflow: hidden`, allow vertical scroll.
2. `#player` (formerly full-screen fixed): change to `position: sticky;
   top: 0; height: 100vh; z-index: 10` so the video sticks at the top
   while scrolling reveals the info below.
3. `#tab-panes`: keep `position: absolute; inset: 0` inside `#player`.
4. `#info-panel`: change from `position: fixed; bottom: 0` to a normal
   block element after `#player` in the DOM. Remove the slide-up
   transform and `body.info-open` toggling. Keep the styling (dark bg,
   padding, flex-column layout).
5. Remove the "ⓘ Info" chrome button (no longer needed to toggle).
   Keep keyboard shortcut `I` to scroll to the info section.
6. Update `player.html`'s OOB swap target: `#info-panel` remains the
   ID, so htmx OOB swaps still work. The content is injected into the
   always-visible panel.
7. `#info-btn` chrome button: repurpose as a "scroll to info" button
   (scrolls `#info-panel` into view via JS) rather than toggling
   visibility.

## Edge cases
- When no video is selected, info panel shows the default placeholder.
- When a new video is opened, the panel updates via htmx OOB — user
  may scroll up to see the video; the panel is always below.
- The `body.info-open` class and related transitions can be removed
  from CSS to reduce clutter.
