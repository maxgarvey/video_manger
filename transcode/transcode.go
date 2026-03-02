// Package transcode wraps ffmpeg operations used by the video manager.
// Each exported function acquires a slot from sem before invoking ffmpeg so
// that the caller can limit concurrent transcoding jobs.
package transcode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Format describes an ffmpeg output format.
type Format struct {
	Ext       string   // file extension including leading dot, e.g. ".mp4"
	VideoArgs []string // ffmpeg codec args for the video stream
	AudioArgs []string // ffmpeg codec args for the audio stream
}

// Formats is the set of supported output formats.
var Formats = map[string]Format{
	"mp4":  {".mp4", []string{"-c:v", "libx264"}, []string{"-c:a", "aac"}},
	"webm": {".webm", []string{"-c:v", "libvpx-vp9"}, []string{"-c:a", "libopus"}},
	"mkv":  {".mkv", []string{"-c:v", "copy"}, []string{"-c:a", "copy"}},
}

// Convert re-encodes src to dst using the given Format.
// sem is a concurrency-limiting channel; one slot is consumed for the duration
// of the ffmpeg call. bgCtx should be a context.WithoutCancel-derived context
// so that a browser disconnect doesn't kill the job mid-way.
func Convert(bgCtx context.Context, sem chan struct{}, src, dst string, f Format) error {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-bgCtx.Done():
		return fmt.Errorf("request cancelled")
	}
	args := []string{"-y", "-i", src}
	args = append(args, f.VideoArgs...)
	args = append(args, f.AudioArgs...)
	args = append(args, dst)
	return run(bgCtx, args...)
}

// ExportUSB re-encodes src to dst as H.264+AAC MP4 optimised for USB playback.
func ExportUSB(bgCtx context.Context, sem chan struct{}, src, dst string) error {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-bgCtx.Done():
		return fmt.Errorf("request cancelled")
	}
	return run(bgCtx,
		"-y", "-i", src,
		"-c:v", "libx264", "-profile:v", "high", "-level", "4.1",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		dst,
	)
}

// Trim copies a time range of src to dst using stream-copy (-c copy).
// start and end are ffmpeg time strings (e.g. "00:01:30", "90"). If end is
// empty the trim runs to the end of the source file.
func Trim(bgCtx context.Context, sem chan struct{}, src, dst, start, end string) error {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-bgCtx.Done():
		return fmt.Errorf("request cancelled")
	}
	args := []string{"-y", "-ss", start}
	if end != "" {
		args = append(args, "-to", end)
	}
	args = append(args, "-i", src, "-c", "copy", dst)
	return run(bgCtx, args...)
}

// run executes ffmpeg with the given arguments and returns a combined
// stderr message on failure.
func run(ctx context.Context, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", args...) //nolint:gosec
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\nstderr: %s", err, stderr.String())
	}
	return nil
}
