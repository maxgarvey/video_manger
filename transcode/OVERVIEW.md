# transcode — FFmpeg Transcoding & Thumbnails

Package `transcode` wraps `ffmpeg` for video conversion, trimming, watermark removal, USB export, and thumbnail generation. All long-running operations report progress via a callback and are designed to be run in background goroutines.

## Purpose

Provides a typed, testable interface over raw ffmpeg invocations. Callers acquire a semaphore slot before calling conversion functions so the application never runs more than `convertConcurrent` (2) ffmpeg processes simultaneously.

## Supported Formats

The exported `Formats` map and `FormatList` (canonical UI order) define the conversion targets:

| Key | Label | Codec | Notes |
|---|---|---|---|
| `mp4-h264` | MP4 (H.264) | libx264 + AAC | Widest device compatibility |
| `mp4-h265` | MP4 (H.265) | libx265 + AAC | ~40% smaller than H.264 |
| `webm-vp9` | WebM (VP9) | libvpx-vp9 + Opus | Royalty-free, browser-native |
| `mkv-copy` | MKV (stream copy) | `-c copy` | Lossless remux, very fast |

Each format has three quality presets (CRF values): `fast`, `balanced`, `quality`.

## Functions

### ConvertProgress(ctx, src, dst, format, quality, totalSecs, send)

Transcodes `src` to `dst` in the given format/quality. `send` is called with each progress line so the handler can forward it to the SSE client.

Progress is parsed from ffmpeg's `-progress pipe:1` output: `frame`, `fps`, `out_time`, `progress=end`. When `totalSecs` is known, a percentage is computed and prepended to each line.

**The caller must acquire the semaphore before calling this function.**

---

### ExportUSB(bgCtx, sem, src, dst)

Re-encodes to H.264 + AAC MP4 with `-movflags +faststart` (moves the MOOV atom to the front so playback starts immediately on USB-attached devices and smart TVs).

**Acquires the semaphore internally** — do not hold it before calling.

---

### Trim(bgCtx, sem, src, dst, start, end)

Stream-copies (`-c copy`) the segment from `start` to `end` into a new file. No re-encode; fast and lossless. Accepts ffmpeg time strings (`"00:01:30"` or `"90"`).

**Acquires the semaphore internally.**

---

### Delogo(bgCtx, sem, src, dst, x, y, w, h, fillColor)

Removes a watermark by drawing a solid rectangle over it with the `drawbox` filter, then re-encoding with libx264. `fillColor` is a hex color string (e.g. `"000000"` for black).

**Acquires the semaphore internally.**

---

### GenerateThumbnail(src, dst, position)

Extracts a single frame at `position` (0.0–1.0, relative to duration) and saves it as a 320 px wide JPEG (`-q:v 2`). Used by `library.go` during directory sync.

The caller computes a random position in [0.1, 0.9] to avoid black leader frames at the start and credits at the end.

## Semaphore Convention

The application-wide `convertSem` channel (size 2, defined in `server.go`) gates all transcoding:

```go
// acquire before long encode
srv.convertSem <- struct{}{}
defer func() { <-srv.convertSem }()

transcode.ConvertProgress(ctx, src, dst, format, quality, dur, send)
```

`ExportUSB`, `Trim`, and `Delogo` acquire the semaphore themselves because they are called from handler goroutines that do not need it for any other purpose.

## Progress Streaming

```
ffmpeg spawned with -progress pipe:1
        │
        ▼
stdout line-by-line ──► parseHMS(out_time) ──► percent ──► send("42% — 00:00:30")
        │
        └── on progress=end ──► send("done")
```

The `send` callback is provided by `handlers_conversion.go`, which forwards strings to the SSE channel for that job.

## External Dependencies

| Binary | Used by |
|---|---|
| `ffmpeg` | All functions |

If `ffmpeg` is not installed, functions return an error immediately. The UI in `handlers_conversion.go` checks for this before offering conversion options.

## Helper

`parseHMS(s string) float64` converts `"HH:MM:SS.ffffff"` to total seconds. Used internally to turn ffmpeg progress timestamps into percentages.

## References

- Called by: `handlers_conversion.go` (conversion, trim, delogo, USB export), `library.go` (thumbnail generation)
- Companion package for metadata-only writes: [`metadata/`](../metadata/OVERVIEW.md)
