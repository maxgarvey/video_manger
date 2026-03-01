# Plan: Multi-tab info panel fix (#45)

When multiple tabs are open, `#info-panel` is populated via OOB swap when a tab is
opened (player.html has `<div id="info-panel" hx-swap-oob="true">`). Switching
between existing tabs (via `activateTab`) does not update the panel — it stays locked
on the last-opened video.

## Fix

In the `activateTab` JS function, when switching to a tab, re-fetch the player HTML
for that video and process its OOB swaps (i.e. update `#info-panel`). Each tab's
`pane` element already holds the videoId.

Approach: store the videoId on the pane element as a `data-video-id` attribute.
In `activateTab`, fetch `/play/{videoId}` for the newly activated tab, parse the
response, and extract + apply only the OOB swap elements (same logic as `openTab`'s
OOB handling). Don't re-insert the pane — only process the OOB parts.

This means an extra network round-trip on tab switch, but keeps the panel in sync.

```js
async function activateTab(videoId) {
  // ... existing active class toggle + pause logic ...

  // Refresh the info panel for the newly-active tab.
  const resp = await fetch('/play/' + videoId);
  const html = await resp.text();
  const frag = document.createElement('template');
  frag.innerHTML = html;
  frag.content.querySelectorAll('[hx-swap-oob],[data-hx-swap-oob]').forEach(function(el) {
    const targetId = (el.getAttribute('hx-swap-oob') || el.getAttribute('data-hx-swap-oob'))
      .replace(/^(true|innerHTML|outerHTML):?/, '');
    const target = targetId ? document.getElementById(targetId) : document.getElementById(el.id);
    if (target) { target.innerHTML = el.innerHTML; htmx.process(target); }
  });
}
```
