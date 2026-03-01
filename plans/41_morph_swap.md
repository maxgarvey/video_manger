# Plan: idiomorph morph swap for video list (#41)

The 60s poll replaces the entire `#video-list` innerHTML, which resets scroll
position. Using htmx's idiomorph extension preserves DOM nodes in place (diffing
instead of replacing), keeping scroll and focus state.

## Implementation

1. Add the htmx morph extension script after htmx in index.html:
   `<script src="https://unpkg.com/htmx-ext-morph@2.0.1"></script>`

2. Add `hx-ext="morph"` and `hx-swap="morph:innerHTML"` to `#video-list`.

The morph extension works at the element level, so `hx-ext="morph"` on the
container enables it for that element's swaps.

Note: this only helps the 60s background poll. Explicit user actions (tag click,
search) can still do a full swap since the user triggered them intentionally.
