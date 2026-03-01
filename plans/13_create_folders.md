# Plan: Create New Folders In-App (Feature 13)

## Behaviour Change

Users can create a directory on disk directly from the UI, then it is
automatically registered in the library. This removes the friction of
having to shell out to create a folder before adding it.

## Implementation

### New route

```
POST /directories/create
```

Handler:
1. Read `path` from form.
2. Call `os.MkdirAll(path, 0755)`.
3. If that succeeds (or dir already exists), call `store.AddDirectory` + `syncDir`.
4. Return updated directory list (same as `handleAddDirectory`).

### UI change (`templates/index.html`)

Add a "New folder" button next to the existing "Add" button in the
Directories section. When clicked it toggles a small inline form with
a path input and "Create & Add" submit.

The simplest approach: add a second form below the existing one with
`hx-post="/directories/create"` targeting `#directories`.

## Tests

- `TestHandleCreateDirectory_Success` — POST creates dir on disk, adds to DB.
- `TestHandleCreateDirectory_AlreadyExists` — existing dir is fine (MkdirAll is idempotent).
- `TestHandleCreateDirectory_EmptyPath` — 400 Bad Request.
