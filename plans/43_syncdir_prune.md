# Plan: syncDir prune deleted files (#43)

When a file is removed from disk, `syncDir` currently leaves the database record in
place. The video still appears in the library; clicking it serves 404.

## Implementation

After the WalkDir loop, query `ListVideosByDirectory` for the directory, stat each
video's file path, and call `DeleteVideo` for any that no longer exist on disk.

Do this in a second pass after the walk so upserts from the walk are already
committed.

Note: This runs every 60s via the poller and also on manual rescan. Deleting a file
that's currently playing in a browser tab will make the progress endpoint return 404,
but the <video> element already has the stream buffered or will fail gracefully.
