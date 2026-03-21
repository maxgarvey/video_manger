// Package transcode wraps ffmpeg operations used by the video manager.
// Each exported function acquires a slot from sem before invoking ffmpeg so
// that the caller can limit concurrent transcoding jobs.
package transcode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Format describes an ffmpeg output format.
type Format struct {
	Label       string            // human-readable name, e.g. "MP4 — H.264 + AAC"
	Description string            // one-line description for the UI
	Ext         string            // file extension including leading dot, e.g. ".mp4"
	VideoArgs   []string          // ffmpeg codec args for the video stream
	AudioArgs   []string          // ffmpeg codec args for the audio stream
	CRF         map[string]string // quality preset → crf value; nil = no CRF (stream copy)
}

// Formats is the set of supported output formats.
var Formats = map[string]Format{
	"mp4-h264": {
		Label:       "MP4 — H.264 + AAC",
		Description: "Most compatible — plays on virtually any device, TV, or browser",
		Ext:         ".mp4",
		VideoArgs:   []string{"-c:v", "libx264"},
		AudioArgs:   []string{"-c:a", "aac"},
		CRF:         map[string]string{"fast": "28", "balanced": "23", "quality": "18"},
	},
	"mp4-h265": {
		Label:       "MP4 — H.265/HEVC + AAC",
		Description: "~40% smaller than H.264; requires a modern device or player",
		Ext:         ".mp4",
		VideoArgs:   []string{"-c:v", "libx265"},
		AudioArgs:   []string{"-c:a", "aac"},
		CRF:         map[string]string{"fast": "32", "balanced": "28", "quality": "22"},
	},
	"webm-vp9": {
		Label:       "WebM — VP9 + Opus",
		Description: "Royalty-free; excellent browser compatibility",
		Ext:         ".webm",
		VideoArgs:   []string{"-c:v", "libvpx-vp9", "-b:v", "0"},
		AudioArgs:   []string{"-c:a", "libopus"},
		CRF:         map[string]string{"fast": "40", "balanced": "33", "quality": "24"},
	},
	"mkv-copy": {
		Label:       "MKV — stream copy",
		Description: "Fast container remux — no re-encode (may fail if codec is incompatible)",
		Ext:         ".mkv",
		VideoArgs:   []string{"-c:v", "copy"},
		AudioArgs:   []string{"-c:a", "copy"},
	},
}

// FormatEntry pairs a format key with its Format for ordered UI display.
type FormatEntry struct {
	Key string
	Format
}

// FormatList is the canonical display order for the UI.
var FormatList = []FormatEntry{
	{"mp4-h264", Formats["mp4-h264"]},
	{"mp4-h265", Formats["mp4-h265"]},
	{"webm-vp9", Formats["webm-vp9"]},
	{"mkv-copy", Formats["mkv-copy"]},
}

// videoArgs builds the video argument slice for f, appending -crf if the
// format has CRF presets and quality is non-empty.
func videoArgs(f Format, quality string) []string {
	args := append([]string(nil), f.VideoArgs...)
	if f.CRF != nil {
		if quality == "" {
			quality = "balanced"
		}
		if crf, ok := f.CRF[quality]; ok {
			args = append(args, "-crf", crf)
		}
	}
	return args
}

// parseHMS parses an ffmpeg progress time string "HH:MM:SS.ffffff" into seconds.
func parseHMS(s string) float64 {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.ParseFloat(parts[0], 64)
	m, _ := strconv.ParseFloat(parts[1], 64)
	sec, _ := strconv.ParseFloat(parts[2], 64)
	return h*3600 + m*60 + sec
}

// ConvertProgress runs the conversion and streams human-readable progress lines
// to send. totalSecs is the source duration used for percentage completion;
// pass 0 if unknown. The caller is responsible for acquiring a semaphore slot.
// dst is removed on error by the caller; this function does not clean it up.
func ConvertProgress(ctx context.Context, src, dst string, f Format, quality string, totalSecs float64, send func(string)) error {
	args := []string{"-y", "-i", src, "-progress", "pipe:1", "-nostats"}
	args = append(args, videoArgs(f, quality)...)
	args = append(args, f.AudioArgs...)
	args = append(args, dst)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", args...) //nolint:gosec
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	start := time.Now()
	sc := bufio.NewScanner(stdout)
	var (
		frameN  float64
		fps     float64
		outSecs float64
	)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "frame":
			frameN, _ = strconv.ParseFloat(val, 64)
		case "fps":
			fps, _ = strconv.ParseFloat(val, 64)
		case "out_time":
			outSecs = parseHMS(val)
		case "progress":
			elapsed := time.Since(start).Seconds()
			if val == "end" {
				send(fmt.Sprintf("✓ Done in %.1fs", elapsed))
			} else {
				var pct string
				if totalSecs > 0 && outSecs > 0 {
					p := outSecs / totalSecs * 100
					if p > 100 {
						p = 100
					}
					pct = fmt.Sprintf(" / %.0f%%", p)
				}
				send(fmt.Sprintf("frame %d / %.1f fps / %.1fs elapsed%s",
					int(frameN), fps, elapsed, pct))
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%w\nstderr: %s", err, stderr.String())
	}
	return nil
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

// Delogo paints a solid-colour rectangle over a watermark region by re-encoding
// the video with ffmpeg's drawbox filter.
func Delogo(bgCtx context.Context, sem chan struct{}, src, dst string,
	x, y, w, h int, fillColor string) error {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-bgCtx.Done():
		return fmt.Errorf("request cancelled")
	}
	vf := fmt.Sprintf("drawbox=x=%d:y=%d:w=%d:h=%d:color=%s:t=fill", x, y, w, h, fillColor)
	return run(bgCtx, "-y", "-i", src, "-vf", vf, "-c:v", "libx264", "-c:a", "copy", dst)
}

// GenerateThumbnail extracts a frame from the video at the given position (0–1
// relative to duration) and saves it as a JPEG thumbnail.
func GenerateThumbnail(src, dst string, position float64) error {
	if position < 0 {
		position = 0
	} else if position > 1 {
		position = 1
	}

	// Determine duration via ffprobe so we can pass an absolute seek time.
	seekSecs := 0.0
	if out, err := exec.Command("ffprobe", //nolint:gosec
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "format=duration",
		src,
	).Output(); err == nil {
		var result struct {
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if json.Unmarshal(out, &result) == nil {
			if d, _ := strconv.ParseFloat(result.Format.Duration, 64); d > 0 {
				seekSecs = d * position
			}
		}
	}

	args := []string{
		"-ss", fmt.Sprintf("%.3f", seekSecs), // seek before -i for speed
		"-i", src,
		"-vf", "scale=320:-1",
		"-vframes", "1",
		"-q:v", "2",
		"-y",
		dst,
	}
	return run(context.Background(), args...)
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
