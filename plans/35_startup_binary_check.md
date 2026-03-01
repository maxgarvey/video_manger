# Plan: Startup binary check (#35)

On startup, check that `ffmpeg`, `ffprobe`, and `yt-dlp` are on PATH using
`exec.LookPath`. Log a warning for each missing binary — not a fatal error,
because the app is usable without them (just some features degrade).

## Implementation

Add a `checkBinaries()` call in `main()` just before starting the server.

```go
func checkBinaries() {
    for _, bin := range []string{"ffmpeg", "ffprobe", "yt-dlp"} {
        if _, err := exec.LookPath(bin); err != nil {
            log.Printf("WARNING: %s not found in PATH — related features will be unavailable", bin)
        }
    }
}
```

## Tests

No new test needed — `exec.LookPath` behaviour is well-tested by stdlib and the
binary presence in the test environment is irrelevant. The existing tests already
exercise the graceful degradation paths (500 on missing ffmpeg).
