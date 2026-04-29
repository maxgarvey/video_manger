# roku — Roku SceneGraph Channel

A sideloaded Roku channel that browses and streams videos from the Go server over the local network.

## Purpose

Enables playback directly on a Roku-connected TV without needing a browser or screen mirroring. The channel communicates with the Go server's JSON API (`/api/*`) and can receive "cast" commands pushed from the web UI.

## Directory Structure

```
roku/
├── manifest                  ← Channel metadata (title, icons, firmware version)
├── source/
│   ├── main.brs              ← Channel entry point (Roku firmware calls this)
│   └── api.brs               ← HTTP/JSON helpers shared by all components
├── components/               ← SceneGraph XML + BrightScript component files
│   ├── MainScene.xml/.brs    ← Root scene; owns navigation stack
│   ├── ShowGrid.xml/.brs     ← Thumbnail grid of shows
│   ├── SeasonGrid.xml/.brs   ← Episode list for a season
│   └── VideoDetails.xml/.brs ← Full episode info + play button
└── images/                   ← Channel icons (SD and HD)
```

## Architecture

The channel uses the **Roku SceneGraph** framework (XML component trees + BrightScript). Components communicate through `m.top` fields (the public interface of a node) and tasks (`Task` nodes for background HTTP work).

### Component Hierarchy

```
MainScene
└── roSGScreen (firmware-managed navigation stack)
    ├── ShowGrid          ← /api/shows
    ├── SeasonGrid        ← /api/shows/{show}/seasons/{season}/episodes
    ├── VideoDetails      ← selected episode data
    └── Video player      ← native Roku video node
```

### Data Flow: Browse

1. `MainScene` initializes and loads `ShowGrid`
2. `ShowGrid` calls `httpGetJSON("/api/shows")` → renders thumbnail grid (poster + show name)
3. User selects a show → `MainScene` pushes `SeasonGrid` with the selected show name
4. `SeasonGrid` calls `httpGetJSON("/api/shows/{show}/seasons/{season}/episodes")` → renders episode list
5. User selects an episode → `MainScene` pushes `VideoDetails` with full episode data
6. User presses play → video URL (`/video/{id}`) loaded into the native Roku video player

### Data Flow: Cast (Web UI → Roku)

1. User clicks "Cast" on the web UI → `POST /roku/cast/{id}` (Go server)
2. Roku channel polls `GET /roku/poll` every 2 seconds
3. On first poll with a pending video, server returns video JSON and clears the queue
4. Channel immediately loads the video in the player without navigating menus

The web UI shows a "Roku connected" indicator by polling `GET /roku/connected`, which returns 200 if the Roku has polled within the last 6 seconds.

## Source Files

### source/main.brs

Channel entry point called by Roku firmware at launch:

1. Creates `roSGScreen` (the SceneGraph compositor)
2. Instantiates `MainScene` and attaches it to the screen
3. Runs the standard Roku message loop (blocks on `wait()`, exits on screen close)

### source/api.brs

Shared HTTP utilities used by all SceneGraph components:

| Function | Description |
|---|---|
| `httpGetJSON(url)` | Synchronous GET → parsed JSON object/array, or `Invalid` on error |
| `httpPost(url, body)` | Synchronous POST with `application/x-www-form-urlencoded` body |
| `urlEncode(s)` | Percent-encodes a string for use in URLs |
| `formatDuration(secs)` | Formats seconds as `H:MM:SS` or `M:SS` for display |

All requests include `Accept-Encoding: gzip` and `Accept: application/json`. Both HTTP and HTTPS are supported (the Roku CA bundle is trusted). The channel always connects to the plain HTTP port (8080) since Roku devices cannot accept self-signed TLS certificates.

## Server API Endpoints Used

| Endpoint | Used by |
|---|---|
| `GET /api/shows` | ShowGrid — full show list with thumbnails |
| `GET /api/shows/{show}/seasons` | SeasonGrid — season list |
| `GET /api/shows/{show}/seasons/{season}/episodes` | SeasonGrid — episode list |
| `GET /api/random` | Quick-play random unwatched video |
| `GET /roku/poll` | Cast queue polling (every 2 s) |
| `GET /roku/connected` | Liveness check from web UI |
| `GET /video/{id}` | Video stream (HTTP range requests) |
| `GET /videos/{id}/thumbnail` | Thumbnail images |

## Deployment

The channel is sideloaded (not distributed through the Roku Channel Store):

```
make roku-deploy ROKU_IP=192.168.86.35 ROKU_PASS=rokudev
```

This zips the channel directory and uploads it to the Roku device's developer installer. The device must be in developer mode (`Settings → System → Advanced → Developer mode`).

`make roku` alone builds the zip without deploying.

## Known BrightScript Gotchas

These are documented because they caused real bugs and are non-obvious:

| Symptom | Root Cause | Fix |
|---|---|---|
| Channel crashes with `&hf4` | `m.top.Exit()` is not a valid call | Use `m.top.exitChannel = True` |
| Top-level assignments crash compilation | SceneGraph prohibits top-level code outside functions | Move all initialization into `Sub init()` |
| `type="dynamic"` field invalid in XML | Not a valid SceneGraph field type | Use `type="string"` and pass raw JSON, parse with `ParseJSON()` |
| Task node does nothing | Missing `functionName` assignment | Set `m.top.functionName = "runCPUTask"` in `Sub init()` |
| Widgets lose focus after `popView()` | Restored node is not re-activated | Toggle `isActive` False → True on the restored node |
| SSE events never arrive | Gzip middleware buffers the response | Register SSE routes outside the `Compress` middleware group |

## mDNS Discovery

When `roku_enabled=true`, the Go server registers `_http._tcp.local.` via zeroconf so the Roku channel can discover the server address without a hardcoded IP. The channel falls back to the last-known IP if mDNS resolution fails.

## References

- Cast queue (server side): `handlers_roku.go` in the root package
- JSON API: `handlers_api.go` in the root package
- Deploy target: `Makefile` (`roku`, `roku-deploy`)
- Device IP in use: `192.168.86.35`; server LAN IP: `192.168.86.26:8080`
