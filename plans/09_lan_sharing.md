# Plan: LAN Sharing — Show Local Network URL (Feature 9)

## Behaviour Change

The server already listens on `0.0.0.0` (all interfaces) so it is
accessible from any device on the same LAN. The only missing piece
is telling the user what URL to visit on other devices.

## Implementation

### Server startup
On startup, detect the machine's non-loopback IPv4 addresses and
log them all. Also expose them via `GET /info` as JSON.

### UI — Settings panel
In the settings panel, show a "LAN Access" section with the detected
local IP URLs. Use a small `<div>` that loads from `GET /info`.

### `GET /info`
Returns JSON:
```json
{"port": "8080", "addresses": ["http://192.168.1.42:8080"]}
```

### `templates/lan_info.html`
Renders the address list as copyable links.

## Tests
- `TestHandleInfo` — 200, JSON with port key.
