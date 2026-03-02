# Plan: Fix failing tests and CI

## Problem
Two tests fail after the convert UI overhaul (format key rename + async handler):

1. `TestHandleConvert_BadVideo` — sends `format=mp4` (old key, now invalid) →
   gets 400 (unsupported format) instead of 404 (video not found).
2. `TestHandleConvert_NoFFmpeg` — sends `format=mkv` (old key) and expects 500 →
   gets 400; also the new handler is async so the error surfaces via SSE not HTTP status.

Additionally `newTestServer` does not initialise `convertJobs`, which would
panic if a convert job is ever started from a test.

## Fix
- `newTestServer`: add `convertJobs: make(map[string]*convertJob)`
- `TestHandleConvert_SameExtension`: change format `"mkv"` → `"mkv-copy"`
- `TestHandleConvert_BadVideo`: change format `"mp4"` → `"mp4-h264"`
- `TestHandleConvert_NoFFmpeg`: change format `"mkv"` → `"mkv-copy"`;
  update expectation to 200 + EventSource body (async SSE pattern, same as
  `TestHandleYTDLP_NotInstalled`)
