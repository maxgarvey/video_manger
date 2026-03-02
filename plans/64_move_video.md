# Plan: Move video to a different directory

## Goal
From the info panel (while watching a video), allow the user to move the
video file to a different registered directory, or create a new sub-folder
first and then move into it.

## Scope
- "Move to…" button in the info panel opens a compact form.
- Form shows a <select> of registered directories (reuse /directories/options).
- Optional "new sub-folder name" input: if filled in, the server creates the
  sub-directory under the chosen parent before moving.
- On submit: server moves the file on disk (os.Rename, fallback to copy+remove
  across filesystems), updates the DB record (UpdateVideoPath), refreshes the
  video list, re-syncs the affected directories.

## New route
  POST /videos/{id}/move
  Form fields: dir_id (int64), subdir (optional string)

## Handler logic
1. Get video by id.
2. Get target directory by dir_id.
3. If subdir != "": create targetDir/subdir with MkdirAll; register the
   new path as a directory (AddDirectory); use that as the move destination.
4. Build dst = targetDir[/subdir]/filename; guard against overwrite.
5. os.Rename(src, dst); if it fails (cross-device), io.Copy + os.Remove.
6. UpdateVideoPath(ctx, videoID, newDirID, newDirPath, filename).
7. Sync both the old directory (to remove the stale entry) and the new one.
8. Return updated video list fragment.

## UI
- New "📂 Move to…" button in the info panel, below the copy-to-library row.
- Clicking shows an inline form (hidden div) with:
    <select> of directories  +  optional sub-folder input  +  Move button
- On success the form collapses and the video list refreshes.
