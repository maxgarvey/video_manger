# Plan: Drag and drop folders/files to import (#60)

## Goal

Allow the user to drag video files or entire folders from the OS onto
a drop zone in the Library sidebar. Each video file is uploaded and
immediately registered in the chosen directory.

## Server (`main.go`)

- New route: `POST /import/upload`
- Handler `handleImportUpload`:
  1. Parse `dir_id` form field → look up directory.
  2. Parse multipart file (`r.FormFile("file")`).
  3. Write file to `<dir.Path>/<filename>` with counter suffix if needed.
  4. `UpsertVideo` + auto-tag with directory base name.
  5. Return 200 OK (no body needed; JS drives the UI).

## Template (`index.html`)

- Add a collapsible section "Drop to import" in `lib-left`.
- Directory selector (`<select>`) populated via `/directories/options`.
- A styled drop zone div with drag-event listeners.
- JavaScript:
  - `dragover` → prevent default + highlight.
  - `dragleave` / `drop` → remove highlight.
  - `drop`: iterate `dataTransfer.items`, call `collectFiles()` (Promise-based
    recursive `FileSystemEntry` traversal with paginated `readEntries`).
  - Filter to video extensions only.
  - Upload each file with `fetch` + `FormData`.
  - Show "Uploading N / M" progress, then refresh `#video-list` via
    `htmx.ajax('GET', '/videos', …)` on completion.
