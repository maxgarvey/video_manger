package metadata

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseFFProbeOutput(t *testing.T) {
	data := []byte(`{
		"format": {
			"tags": {
				"title":          "My Movie",
				"genre":          "Action",
				"keywords":       "summer,vacation,family",
				"description":    "A great trip",
				"artist":         "John Doe",
				"date":           "2023",
				"show":           "The Show",
				"network":        "HBO",
				"episode_id":     "S01E02",
				"season_number":  "1",
				"episode_sort":   "2"
			}
		}
	}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Title != "My Movie" {
		t.Errorf("Title = %q, want My Movie", m.Title)
	}
	if m.Genre != "Action" {
		t.Errorf("Genre = %q, want Action", m.Genre)
	}
	if m.Description != "A great trip" {
		t.Errorf("Description = %q, want 'A great trip'", m.Description)
	}
	if m.Artist != "John Doe" {
		t.Errorf("Artist = %q, want 'John Doe'", m.Artist)
	}
	if m.Date != "2023" {
		t.Errorf("Date = %q, want 2023", m.Date)
	}
	if len(m.Keywords) != 3 {
		t.Fatalf("expected 3 keywords, got %v", m.Keywords)
	}
	for i, want := range []string{"summer", "vacation", "family"} {
		if m.Keywords[i] != want {
			t.Errorf("Keywords[%d] = %q, want %q", i, m.Keywords[i], want)
		}
	}
	if m.Show != "The Show" {
		t.Errorf("Show = %q, want 'The Show'", m.Show)
	}
	if m.Network != "HBO" {
		t.Errorf("Network = %q, want HBO", m.Network)
	}
	if m.EpisodeID != "S01E02" {
		t.Errorf("EpisodeID = %q, want S01E02", m.EpisodeID)
	}
	if m.SeasonNum != "1" {
		t.Errorf("SeasonNum = %q, want 1", m.SeasonNum)
	}
	if m.EpisodeNum != "2" {
		t.Errorf("EpisodeNum = %q, want 2", m.EpisodeNum)
	}
}

func TestParseFFProbeOutput_SemicolonKeywords(t *testing.T) {
	data := []byte(`{"format": {"tags": {"keywords": "tag1; tag2; tag3"}}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Keywords) != 3 {
		t.Errorf("expected 3 keywords, got %v", m.Keywords)
	}
}

func TestParseFFProbeOutput_Empty(t *testing.T) {
	data := []byte(`{"format": {}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.HasData() {
		t.Errorf("expected empty Meta, got %+v", m)
	}
}

func TestParseFFProbeOutput_FallbackFields(t *testing.T) {
	// artist falls back to album_artist, date falls back to year
	data := []byte(`{"format": {"tags": {"album_artist": "Studio", "year": "2020"}}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Artist != "Studio" {
		t.Errorf("Artist = %q, want Studio", m.Artist)
	}
	if m.Date != "2020" {
		t.Errorf("Date = %q, want 2020", m.Date)
	}
}

func TestHasData(t *testing.T) {
	if (Meta{}).HasData() {
		t.Error("empty Meta.HasData() should be false")
	}
	if !(Meta{Title: "x"}).HasData() {
		t.Error("Meta{Title}.HasData() should be true")
	}
	if !(Meta{Keywords: []string{"a"}}).HasData() {
		t.Error("Meta{Keywords}.HasData() should be true")
	}
	if !(Meta{Show: "My Show"}).HasData() {
		t.Error("Meta{Show}.HasData() should be true")
	}
	if !(Meta{EpisodeID: "S01E01"}).HasData() {
		t.Error("Meta{EpisodeID}.HasData() should be true")
	}
}

// --- ReadDuration ---

// TestReadDuration_NoFFprobe verifies that ReadDuration returns 0 silently
// when ffprobe is not on PATH, instead of panicking or returning an error.
func TestReadDuration_NoFFprobe(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH: no executables
	got := ReadDuration("/any/path.mp4")
	if got != 0 {
		t.Errorf("expected 0 when ffprobe unavailable, got %f", got)
	}
}

// TestReadDuration_MissingFile verifies that ReadDuration returns 0 when
// ffprobe is available but the file does not exist (ffprobe exits non-zero).
func TestReadDuration_MissingFile(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping")
	}
	got := ReadDuration("/nonexistent/no-such-file.mp4")
	if got != 0 {
		t.Errorf("expected 0 for missing file, got %f", got)
	}
}

// TestReadDuration_RealFile verifies that ReadDuration returns a positive
// duration for a real video file. Skipped if ffmpeg/ffprobe are absent.
func TestReadDuration_RealFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping")
	}

	// Generate a 2-second test video with ffmpeg.
	dir := t.TempDir()
	out := filepath.Join(dir, "dur_test.mp4")
	cmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=2",
		"-c:v", "libx264", "-y", out)
	if err := cmd.Run(); err != nil {
		t.Skipf("could not generate test video: %v", err)
	}

	got := ReadDuration(out)
	// Allow ±0.5 s tolerance around the expected 2 s duration.
	if got < 1.5 || got > 2.5 {
		t.Errorf("ReadDuration = %f, want ~2.0", got)
	}
}

// --- T13: Write ---

// TestWrite_NoFFmpeg verifies that Write is a silent no-op when ffmpeg is not
// available on PATH, returning nil rather than an error.
func TestWrite_NoFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH: no executables
	title := "Should Not Error"
	if err := Write("/fake/path.mp4", Updates{Title: &title}); err != nil {
		t.Errorf("expected nil when ffmpeg is unavailable, got: %v", err)
	}
}

// TestWrite_ErrorOnMissingFile verifies that if ffmpeg is available but the
// source file does not exist, Write returns an error.
func TestWrite_ErrorOnMissingFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping")
	}
	title := "Phantom"
	err := Write("/nonexistent/path.mp4", Updates{Title: &title})
	if err == nil {
		t.Error("expected error when source file does not exist, got nil")
	}
}

// TestWrite_RoundTrip verifies that Write can update metadata on a real file
// and the change can be read back by ffprobe. Skipped if either tool is absent.
func TestWrite_RoundTrip(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed; skipping")
	}

	// Create a minimal valid MP4 container using ffmpeg.
	dir := t.TempDir()
	out := filepath.Join(dir, "test.mp4")
	cmd := exec.Command("ffmpeg", "-f", "lavfi", "-i", "nullsrc=s=64x64:d=1", "-c:v", "libx264", "-y", out)
	if err := cmd.Run(); err != nil {
		// If the codec isn't available in this ffmpeg build, skip rather than fail.
		t.Skipf("could not generate test video: %v", err)
	}

	title := "Round Trip Title"
	if err := Write(out, Updates{Title: &title}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify the file still exists after Write (which renames the temp file).
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("file missing after Write: %v", err)
	}

	meta, err := Read(out)
	if err != nil {
		t.Fatalf("Read after Write: %v", err)
	}
	if meta.Title != title {
		t.Errorf("expected title %q after round-trip, got %q", title, meta.Title)
	}
}
