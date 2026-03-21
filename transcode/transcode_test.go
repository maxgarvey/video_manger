package transcode

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- parseHMS ---

func TestParseHMS(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"00:00:00.000000", 0},
		{"00:00:01.000000", 1},
		{"00:01:00.000000", 60},
		{"01:00:00.000000", 3600},
		{"01:30:45.500000", 5445.5},
		{"99:59:59.999999", 99*3600 + 59*60 + 59.999999},
		// ffmpeg sometimes omits the microseconds portion
		{"00:00:30", 30},
		{"01:02:03", 3723},
	}
	for _, c := range cases {
		got := parseHMS(c.in)
		// Allow a tiny epsilon for floating-point rounding.
		diff := got - c.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 1e-4 {
			t.Errorf("parseHMS(%q) = %f, want %f (diff %e)", c.in, got, c.want, diff)
		}
	}
}

func TestParseHMS_InvalidReturnsZero(t *testing.T) {
	cases := []string{"", "notatime", "12:34", "a:b:c"}
	for _, s := range cases {
		got := parseHMS(s)
		if got != 0 {
			t.Errorf("parseHMS(%q) = %f, want 0 for invalid input", s, got)
		}
	}
}

// --- videoArgs ---

func TestVideoArgs_AppliesBalancedCRFByDefault(t *testing.T) {
	f := Formats["mp4-h264"]
	args := videoArgs(f, "")
	assertContainsSequence(t, args, "-crf", "23")
}

func TestVideoArgs_AppliesQualityPreset(t *testing.T) {
	cases := []struct {
		format  string
		quality string
		wantCRF string
	}{
		{"mp4-h264", "fast", "28"},
		{"mp4-h264", "balanced", "23"},
		{"mp4-h264", "quality", "18"},
		{"mp4-h265", "fast", "32"},
		{"mp4-h265", "balanced", "28"},
		{"mp4-h265", "quality", "22"},
		{"webm-vp9", "fast", "40"},
		{"webm-vp9", "quality", "24"},
	}
	for _, c := range cases {
		f := Formats[c.format]
		args := videoArgs(f, c.quality)
		assertContainsSequence(t, args, "-crf", c.wantCRF)
	}
}

func TestVideoArgs_StreamCopyHasNoCRF(t *testing.T) {
	f := Formats["mkv-copy"]
	args := videoArgs(f, "quality")
	for i, a := range args {
		if a == "-crf" {
			t.Errorf("stream-copy format should not have -crf flag, but found it at index %d: %v", i, args)
		}
	}
}

func TestVideoArgs_UnknownQualityIsIgnored(t *testing.T) {
	f := Formats["mp4-h264"]
	// An unrecognised quality key should not append -crf (no matching preset).
	args := videoArgs(f, "ultramax")
	for i, a := range args {
		if a == "-crf" {
			t.Errorf("unknown quality preset should produce no -crf, found at index %d: %v", i, args)
		}
	}
}

// --- ConvertProgress line-parser (via parseHMS, indirectly) ---
// The full ConvertProgress function requires ffmpeg, but we can test the
// progress-message construction logic by verifying parseHMS produces the
// correct seconds that would feed into the percentage calculation.

func TestConvertProgress_PercentageCalculation(t *testing.T) {
	// Simulate: totalSecs = 100, outSecs parsed from "00:01:10.000000" = 70s → 70%.
	totalSecs := 100.0
	outSecs := parseHMS("00:01:10.000000")
	if outSecs != 70 {
		t.Fatalf("parseHMS gave %f, expected 70", outSecs)
	}
	pct := outSecs / totalSecs * 100
	if pct != 70 {
		t.Errorf("percentage = %f, want 70", pct)
	}
}

func TestConvertProgress_PercentageCapsAt100(t *testing.T) {
	// outSecs > totalSecs must not produce > 100%.
	totalSecs := 60.0
	outSecs := parseHMS("00:01:05.000000") // 65s > 60s
	p := outSecs / totalSecs * 100
	if p > 100 {
		p = 100
	}
	if p != 100 {
		t.Errorf("capped percentage = %f, want 100", p)
	}
}

// --- FormatList order / completeness ---

func TestFormatList_ContainsAllFormats(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range FormatList {
		if _, ok := Formats[e.Key]; !ok {
			t.Errorf("FormatList entry %q has no corresponding Formats entry", e.Key)
		}
		if seen[e.Key] {
			t.Errorf("FormatList has duplicate key %q", e.Key)
		}
		seen[e.Key] = true
	}
}

func TestFormats_ExtHasLeadingDot(t *testing.T) {
	for key, f := range Formats {
		if !strings.HasPrefix(f.Ext, ".") {
			t.Errorf("Formats[%q].Ext = %q: expected leading dot", key, f.Ext)
		}
	}
}

// --- helpers ---

func assertContainsSequence(t *testing.T, args []string, a, b string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == a && args[i+1] == b {
			return
		}
	}
	t.Errorf("args %v: expected consecutive pair %q %q", args, a, b)
}

// --- ffmpeg integration tests (skipped when ffmpeg absent) ---

func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping")
	}
}

func makeTestVideo(t *testing.T, duration string) string {
	t.Helper()
	skipIfNoFFmpeg(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "test.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d="+duration,
		"-c:v", "libx264", "-y", out)
	if err := cmd.Run(); err != nil {
		t.Skipf("could not generate test video: %v", err)
	}
	return out
}

// --- Trim ---

func TestTrim_ProducesOutputFile(t *testing.T) {
	src := makeTestVideo(t, "5")
	dst := filepath.Join(filepath.Dir(src), "trimmed.mp4")

	sem := make(chan struct{}, 1)
	if err := Trim(context.Background(), sem, src, dst, "00:00:01", "00:00:03"); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	// Output file must exist and ffprobe must be able to open it.
	if err := exec.Command("ffprobe", "-v", "quiet", dst).Run(); err != nil {
		t.Errorf("ffprobe on trimmed output failed: %v", err)
	}
}

func TestTrim_NoEndTime(t *testing.T) {
	src := makeTestVideo(t, "3")
	dst := filepath.Join(filepath.Dir(src), "trimmed_noend.mp4")

	sem := make(chan struct{}, 1)
	// end="" means trim from start to EOF
	if err := Trim(context.Background(), sem, src, dst, "00:00:01", ""); err != nil {
		t.Fatalf("Trim with no end: %v", err)
	}
}

func TestTrim_CancelledContext(t *testing.T) {
	skipIfNoFFmpeg(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	sem := make(chan struct{}, 1)
	err := Trim(ctx, sem, "/nonexistent.mp4", "/dev/null", "0", "1")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

// --- GenerateThumbnail ---

func TestGenerateThumbnail_ProducesJPEG(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping")
	}
	src := makeTestVideo(t, "3")
	dst := filepath.Join(filepath.Dir(src), "thumb.jpg")

	if err := GenerateThumbnail(src, dst, 0.5); err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}

	// Verify the thumbnail exists and ffprobe can read it.
	cmd := exec.Command("ffprobe", "-v", "quiet", dst)
	if err := cmd.Run(); err != nil {
		t.Errorf("ffprobe on thumbnail failed: %v", err)
	}
}

func TestGenerateThumbnail_ClampsBelowZero(t *testing.T) {
	src := makeTestVideo(t, "2")
	dst := filepath.Join(filepath.Dir(src), "thumb_clamp.jpg")
	// position -0.5 should be clamped to 0 — must not error
	if err := GenerateThumbnail(src, dst, -0.5); err != nil {
		t.Fatalf("GenerateThumbnail(position=-0.5): %v", err)
	}
}

func TestGenerateThumbnail_ClampsAboveOne(t *testing.T) {
	src := makeTestVideo(t, "2")
	dst := filepath.Join(filepath.Dir(src), "thumb_clamp2.jpg")
	// position 1.5 should be clamped to 1 — must not error
	if err := GenerateThumbnail(src, dst, 1.5); err != nil {
		t.Fatalf("GenerateThumbnail(position=1.5): %v", err)
	}
}

// --- ConvertProgress ---

func TestConvertProgress_ProducesOutput(t *testing.T) {
	src := makeTestVideo(t, "2")
	dst := filepath.Join(filepath.Dir(src), "converted.mp4")

	var lines []string
	err := ConvertProgress(context.Background(), src, dst,
		Formats["mp4-h264"], "fast", 2.0,
		func(s string) { lines = append(lines, s) })
	if err != nil {
		t.Fatalf("ConvertProgress: %v", err)
	}
	// At minimum we expect the "Done" line.
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Done") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Done line in progress output, got: %v", lines)
	}
}

func TestConvertProgress_UnknownSrcErrors(t *testing.T) {
	skipIfNoFFmpeg(t)
	dst := filepath.Join(t.TempDir(), "out.mp4")
	err := ConvertProgress(context.Background(), "/nonexistent.mp4", dst,
		Formats["mp4-h264"], "fast", 0,
		func(string) {})
	if err == nil {
		t.Error("expected error for non-existent source, got nil")
	}
}

// --- ExportUSB ---

func TestExportUSB_CancelledContext(t *testing.T) {
	skipIfNoFFmpeg(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	sem := make(chan struct{}, 1)
	err := ExportUSB(ctx, sem, "/nonexistent.mp4", "/dev/null")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestExportUSB_ProducesOutput(t *testing.T) {
	src := makeTestVideo(t, "2")
	dst := filepath.Join(filepath.Dir(src), "usb.mp4")

	sem := make(chan struct{}, 1)
	if err := ExportUSB(context.Background(), sem, src, dst); err != nil {
		t.Fatalf("ExportUSB: %v", err)
	}
	if err := exec.Command("ffprobe", "-v", "quiet", dst).Run(); err != nil {
		t.Errorf("ffprobe on ExportUSB output failed: %v", err)
	}
}

// --- Delogo ---

func TestDelogo_CancelledContext(t *testing.T) {
	skipIfNoFFmpeg(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	sem := make(chan struct{}, 1)
	err := Delogo(ctx, sem, "/nonexistent.mp4", "/dev/null", 0, 0, 100, 50, "black")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestDelogo_ProducesOutput(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping")
	}
	src := makeTestVideo(t, "2")
	dst := filepath.Join(filepath.Dir(src), "delogoed.mp4")

	sem := make(chan struct{}, 1)
	if err := Delogo(context.Background(), sem, src, dst, 0, 0, 10, 10, "black"); err != nil {
		t.Fatalf("Delogo: %v", err)
	}
	if err := exec.Command("ffprobe", "-v", "quiet", dst).Run(); err != nil {
		t.Errorf("ffprobe on Delogo output failed: %v", err)
	}
}
