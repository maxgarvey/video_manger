# Plan: Show codec info via ffprobe (#56)

## Backend (`metadata/metadata.go`)

Add a `ReadStreams(path string) ([]Stream, error)` function that calls
ffprobe with `-show_streams` in addition to the existing format call.

```go
type Stream struct {
    CodecType string // "video" or "audio"
    CodecName string // e.g. "h264", "aac"
    Width, Height int // video only
    FrameRate string  // e.g. "23.976"
    BitRate   string  // bits/s as string
    SampleRate string // audio only
    Channels   int    // audio only
}
```

A new `ReadStreams(path) ([]Stream, error)` function calls ffprobe with
`-show_streams -show_format` and populates the struct from JSON.

## Backend (`main.go`)

`handleGetMetadata` calls `ReadStreams` and passes streams alongside the
existing `Native metadata.Meta` to `file_metadata.html`.

## Frontend (`file_metadata.html`)

Add a second `<dl>` block below the existing tag metadata that shows
stream codec info (codec name, resolution, frame rate, audio channels).
