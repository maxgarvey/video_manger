# electron — Desktop App Wrapper

Wraps the Go HTTP server in an Electron window so the application launches as a native macOS app without manual terminal or port management.

## Purpose

The Go server is a headless process — users normally start it from a terminal and navigate to `localhost:8080` in a browser. Electron eliminates that friction: double-clicking the app (or running `make electron`) starts the server automatically, opens a window, and shuts the server down when the window closes.

## File

`main.js` — Electron main process (Node.js). There is no renderer-process code; the window simply loads the Go server's existing HTML UI over HTTP.

## How It Works

### Startup

1. `app.requestSingleInstanceLock()` — prevents a second instance (two writers against the same SQLite WAL file would corrupt the database). If a second instance is launched, it focuses the existing window instead.
2. `startGoServer()` — spawns the `video_manger` binary as a child process with explicit flags:
   - `--http-port=8080` (fixed; Roku depends on this address)
   - `--https-port=8081`
   - `--db=<userData>/video_manger.db` (writable directory; see DB Path below)
3. `waitForPort(8080)` — polls TCP port 8080 every 250 ms until the Go server accepts connections (15 s timeout). Shows an error dialog and quits if the deadline is exceeded.
4. `createWindow()` — opens a 1400×900 `BrowserWindow` loading `http://localhost:8080`. Plain HTTP is used so there is no self-signed certificate warning inside the Electron window.

### Shutdown

`app.on('before-quit')` sends `SIGTERM` to the Go process, which triggers its graceful shutdown handler (2 s drain, then close). The Go server saves sessions to the DB before exiting so logins persist across app restarts.

### Error Handling

| Condition | Behavior |
|---|---|
| Binary not found (`ENOENT`) | Error dialog: "Run `make build` first" |
| Server doesn't bind in 15 s | Error dialog: port may already be in use |
| Server exits unexpectedly | Error dialog with exit code; app quits |
| Normal shutdown (SIGTERM / exit 0) | Silent |

## Binary Location

| Mode | Path |
|---|---|
| Development (`npm start`) | `../video_manger` (repo root, built by `make build`) |
| Packaged (`.app` bundle) | `Contents/Resources/video_manger` (via `extraResources` in electron-builder config) |

`app.isPackaged` distinguishes the two cases.

## DB Path

In development the server uses `video_manger.db` in the repo root (default flag value). When packaged, Electron overrides `--db` to point at:

```
~/Library/Application Support/Video Manager/video_manger.db
```

The Go server generates `cert.pem` and `key.pem` in the same directory as the DB, so TLS certificates also end up in `userData` automatically.

## Build & Distribution

| Command | Result |
|---|---|
| `make electron` | Build Go binary → `npm start` (dev, requires repo) |
| `make electron-dist` | Build Go binary → `npm run dist` → `dist/Video Manager.dmg` |
| `make electron-install DEST=/Applications` | Build + dist + install `.app` to destination |

### electron-builder Config (`package.json` → `"build"`)

```json
{
  "appId": "com.maxgarvey.video-manger",
  "productName": "Video Manager",
  "mac": { "target": "dmg", "category": "public.app-category.entertainment" },
  "files": ["electron/**/*"],
  "extraResources": [{ "from": "video_manger", "to": "video_manger" }]
}
```

`extraResources` copies the compiled Go binary into the `.app` bundle while preserving execute permissions.

## References

- Go server entry point: [`../main.go`](../OVERVIEW.md)
- Build configuration: [`../package.json`](../package.json), [`../Makefile`](../Makefile)
