# Plan: Video Format Conversion (Feature 15)

## Behaviour Change

From the player info panel, users can transcode the current video to
another format. The output is saved alongside the original and
registered in the library.

## Supported output formats

| Format | Extension | Video codec | Audio codec |
|--------|-----------|-------------|-------------|
| MP4 (H.264) | .mp4 | libx264 | aac |
| WebM (VP9) | .webm | libvpx-vp9 | libopus |
| MKV (copy) | .mkv | copy | copy |

"copy" just remuxes without re-encoding — very fast.

## API

```
POST /videos/{id}/convert   body: format=<mp4|webm|mkv>
```

Runs ffmpeg synchronously (same pattern as USB export), saves output
next to the original with the new extension, registers it in the
library via UpsertVideo, returns the updated video list.

## UI

In player info panel, a "Convert" section with format select and
Convert button. On completion, the video list refreshes and the
new file appears.

## Tests

- `TestHandleConvert_InvalidFormat` — 400 for unknown format.
- `TestHandleConvert_BadVideo` — 404 for unknown video.
- `TestHandleConvert_NoFFmpeg` — 500 when ffmpeg not in PATH.
