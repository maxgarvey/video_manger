# Plan: handleConvert background context (#44)

`exec.CommandContext(r.Context(), "ffmpeg", ...)` in `handleConvert` means if the
browser disconnects (tab closed, navigation away), the request context is cancelled
and ffmpeg is killed mid-conversion, leaving a partial/corrupt output file.

The same applies to `handleExportUSB`.

## Fix

Use `context.WithoutCancel(r.Context())` (Go 1.21+) to decouple the ffmpeg process
lifetime from the HTTP request. The conversion will complete even if the browser
disconnects.

For `handleTrim` this is less critical since it uses `-c copy` (fast), but apply the
same fix for consistency.

Note: this means a conversion can run after the HTTP response has been sent. The
caller gets an immediate response (success or error based on process start), then the
file appears in the library when the poller next runs. A more complete solution would
use SSE/job polling, but that is a larger change (item 50).
