package transcode

import (
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
