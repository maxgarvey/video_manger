# Plan: P2P Sharing (Feature 15)

## Behaviour

A "📤 Share" button in the player info panel opens an inline share panel
showing all URLs from which the video can be streamed directly — the LAN
IP addresses and mDNS name already reported by `/info`. Each URL links to
`/video/{id}` (the raw byte-range-capable endpoint), enabling any device
on the same network to play the video directly from this machine.

This is genuine peer-to-peer sharing: the file streams from this device
to another with no relay or upload step.

## New route

`GET /videos/{id}/share` → returns an HTML fragment (share_panel.html)
inserted into `#share-modal-{id}` in the info panel.

The fragment lists:
- Each LAN address as a clickable `<a>` link to `/video/{id}`
- The mDNS URL if available
- A JS "Copy" button for each link

## UX change

`templates/player.html` info panel gains:

```
📤 Share   (button, hx-get="/videos/{id}/share" hx-target="#share-modal-{id}")
<div id="share-modal-{id}"></div>
```

## Template

`templates/share_panel.html` — receives:

```go
struct {
    VideoID   int64
    Links     []string   // full http:// streaming URLs
}
```

Renders each URL as a row with an anchor + a JS clipboard copy button.
If `Links` is empty, shows a "No network interfaces found" message.

## Tests

- `TestHandleSharePanel_OK` — known video, returns 200 with video ID in body.
- `TestHandleSharePanel_BadVideo` — unknown video, returns 404.
