# Plan: Recursive directory scan with progress feedback (#58)

The scan already walks directories recursively (filepath.WalkDir). The
UX problem is there is no visible feedback while a large scan runs.

## Approach

Run `syncDir` asynchronously in a goroutine so HTTP returns immediately.
Track which directories are currently syncing in the server struct.
The directories template polls every 2 s while any sync is in progress,
showing a spinner next to syncing entries.

## Server changes

- Add `syncingDirs map[int64]struct{}` + mutex to `server` struct.
- `handleSyncDirectory`: set syncing=true, start goroutine, return
  current dir list immediately.
- `addAndSyncDir`: same — goroutine for sync, respond right away.
- `serveDirList`: pass `SyncingIDs map[int64]bool` to the template.

## Template (`directories.html`)

- Per-row spinner when `SyncingIDs[.ID]` is true.
- When any sync is in progress, add `hx-trigger="every 2s"` to the
  `#directories` div so it auto-polls; when all syncs finish the
  polling trigger is absent and polling stops.
