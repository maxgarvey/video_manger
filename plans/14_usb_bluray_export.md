# Plan: USB/BluRay Export (Feature 14)

## Behaviour Change

From the info panel, users can export the currently playing video to a
USB-compatible format that BluRay players can read.

BluRay players typically support:
- **AVCHD**: H.264 video in an AVCHD directory structure (complex)
- **Simpler**: standard MP4 container with H.264 + AAC, constrained
  to profile/level the player can handle

The pragmatic approach is the "DLNA-compatible MP4":
```
ffmpeg -i input -c:v libx264 -profile:v high -level 4.1
       -c:a aac -b:a 192k
       -movflags +faststart
       output.mp4
```

This works on the vast majority of BluRay players with USB playback.

## UI

In the player info panel, add an "Export for USB/TV" button.
Clicking it:
1. POSTs to `/videos/{id}/export/usb`
2. Handler runs ffmpeg in the background (or synchronously with timeout)
3. On completion, returns a link to download the output file

Since export can take a long time, it runs asynchronously:
- POST starts the export, returns immediately with a job ID
- A status endpoint polls until done
- UI shows "Exporting…" then a download link

For simplicity in this implementation: run synchronously with a long
timeout and stream the output file directly as a download response.

## Implementation

### Routes
```
POST /videos/{id}/export/usb   — start export, stream output as download
```

### Handler
- Look up the video.
- Create a temp output file: `<name>_usb.mp4` in the same directory.
- Run `ffmpeg -i <input> -c:v libx264 -profile:v high -level 4.1
  -c:a aac -b:a 192k -movflags +faststart <output>`.
- On success, serve the output file as a download (`Content-Disposition: attachment`).
- On failure, return 500 with ffmpeg stderr.

### UI (player.html)
Add export button in info panel below rating buttons.

## Tests
- `TestHandleExportUSB_NoFFmpeg` — if ffmpeg not found, 500 is returned gracefully.
- `TestHandleExportUSB_BadVideo` — invalid video ID → 404.
