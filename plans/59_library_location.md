# Plan: Configurable library location + copy files to library (#59)

## Goal

Allow the user to designate a "library" folder in Settings. Any video
can then be copied into that folder with one click from the info panel.

## Changes

### Settings (`handleGetSettings`, `handleSaveSettings`, `settings.html`)

- Read/write a new DB setting key `library_path`.
- Add a text input to the settings form for the library path.
- Allow leaving blank to clear.

### New route & handler (`main.go`)

- `POST /videos/{id}/copy-to-library` → `handleCopyToLibrary`
  1. Get `library_path` from settings; error if empty.
  2. Resolve source path from the video record.
  3. Ensure library directory exists (`os.MkdirAll`).
  4. Build destination `<library_path>/<filename>`; append `_2`, `_3`, …
     suffix if a file with that name already exists (same pattern as trim).
  5. Stream copy with `io.Copy`.
  6. Respond with a small success/error HTML snippet that replaces the
     button's target div.

### Template (`player.html`)

- Add a "📋 Copy to library" button below Export/Convert.
- Only rendered when `{{.LibraryPath}}` is non-empty.
- `hx-target="#copy-lib-status-{{.Video.ID}}"` for inline feedback.
