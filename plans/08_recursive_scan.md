# Plan: Recursive Directory Scan (Feature 8)

## Behaviour Change

Currently `syncDir` uses `os.ReadDir` which only reads a single level.
After this change it walks the full directory tree using `filepath.WalkDir`.

Key decisions:
- **Always recursive** — no flag; if you add a directory you want all its videos.
- **One directory DB entry per registered path** — subdirectories are NOT
  registered as separate `directories` rows. All videos found under a
  registered root keep that root's `directory_id`.
- **directory_path = actual parent dir** — each video stores the real
  subdirectory it lives in (not the root), so `FilePath()` resolves correctly.
- **Re-sync is idempotent** — `UpsertVideo` ON CONFLICT DO UPDATE means
  re-adding a directory or calling syncDir again is safe.

## Implementation

### `main.go` — `syncDir`

Replace `os.ReadDir` loop with `filepath.WalkDir`:

```go
func (s *server) syncDir(d store.Directory) {
    filepath.WalkDir(d.Path, func(path string, de fs.DirEntry, err error) error {
        if err != nil {
            log.Printf("sync walk %s: %v", path, err)
            return nil // keep walking
        }
        if de.IsDir() || !isVideoFile(de.Name()) {
            return nil
        }
        dir := filepath.Dir(path)
        v, err := s.store.UpsertVideo(context.Background(), d.ID, dir, de.Name())
        if err != nil {
            log.Printf("upsert %s: %v", path, err)
            return nil
        }
        if v.DisplayName == "" {
            if meta, err := metadata.Read(path); err == nil && meta.Title != "" {
                if err := s.store.UpdateVideoName(context.Background(), v.ID, meta.Title); err != nil {
                    log.Printf("set native title %s: %v", path, err)
                }
            }
        }
        return nil
    })
}
```

Need to add `"io/fs"` to imports.

## Tests

- `TestSyncDir_Recursive` — create a temp dir with nested subdirectories
  containing video files, call `syncDir`, verify all videos appear in
  `ListVideos` with correct `FilePath()` values.
- `TestSyncDir_NonVideo` — verify non-video files in subdirs are ignored.
- Existing tests are unaffected (they don't call `syncDir`).
