# Plan: Auto-tag Videos with Directory Name (Feature 3)

## Behaviour Change

When a video is upserted during `syncDir`, automatically create a tag
whose name is the **base name of the registered root directory** and
apply it to the video. This lets users filter the library by folder
with one click.

Examples:
- Register `/home/user/Movies/Action` → tag `Action` applied to every video inside
- Register `/mnt/nas/TV` → tag `TV` applied to every video inside

Decisions:
- Use the **registered root directory's base name** (not the subdirectory),
  so all files in a recursive scan share the same folder-level tag.
- Tags are upserted (idempotent), so re-syncing doesn't duplicate.
- Existing tags on a video are not disturbed; only the directory tag is added.

## Implementation

### `main.go` — `syncDir`

After `UpsertVideo`, also upsert the directory-name tag and apply it:

```go
dirTag, err := s.store.UpsertTag(context.Background(), filepath.Base(d.Path))
if err != nil {
    log.Printf("upsert dir tag %s: %v", d.Path, err)
} else {
    s.store.TagVideo(context.Background(), v.ID, dirTag.ID)
}
```

Add `syncTagsToFile` call here too so the tag is written back to the file.

## Tests

- `TestSyncDir_AutoTagsByDirectoryName` — syncDir creates a tag matching
  the directory base name and applies it to all synced videos.
- `TestSyncDir_AutoTag_Idempotent` — running syncDir twice doesn't
  create duplicate tags.
