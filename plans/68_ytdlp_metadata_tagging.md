# Plan: yt-dlp metadata tagging on import

## Problem
After a yt-dlp download completes the video file has no embedded metadata.
The user requested that the downloaded metadata be used to tag the file.

## Approach
1. Add `--write-info-json` and `--no-write-thumbnail` flags to the yt-dlp command.
2. Capture the output video file path by watching for the line:
   `[download] Destination: /abs/path/to/file.ext`
   (also handle the "already downloaded" variant)
3. After successful download, read `<videoPath>.info.json`.
4. Parse it into a `metadata.Updates` struct (title, description,
   upload_date→date, channel/uploader→network, tags→keywords,
   genre, series→show, season_number, episode_number).
5. Call `metadata.Write(videoPath, updates)` to embed tags in the file
   (stream-copy, no re-encode).
6. Delete the `.info.json` file (cleanup).
7. Then call `s.syncDir(dir)` as before.

## Files changed
- `handlers.go`: add `--write-info-json` / `--no-write-thumbnail` args,
  capture video path in scan loop, add `parseYTDLPInfoJSON` helper,
  call Write + cleanup after success.

## Cleanup note
`--no-write-thumbnail` suppresses thumbnail files.
The .info.json is deleted after parsing.
No other side-files are expected.
