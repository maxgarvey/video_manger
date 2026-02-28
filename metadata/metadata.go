package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Meta holds native metadata read from a video file via ffprobe.
type Meta struct {
	Title       string
	Description string
	Genre       string
	Keywords    []string
	Artist      string
	Date        string
	Comment     string
}

// HasData reports whether any metadata field is populated.
func (m Meta) HasData() bool {
	return m.Title != "" || m.Description != "" || m.Genre != "" ||
		len(m.Keywords) > 0 || m.Artist != "" || m.Date != ""
}

// Updates holds metadata fields to write back to a file.
// A nil pointer means "leave this field unchanged".
type Updates struct {
	Title    *string  // nil = preserve, "" = clear
	Keywords []string // nil = preserve, []string{} = clear
}

// Read reads native metadata from a video file using ffprobe.
// Returns an empty Meta (no error) if ffprobe is not available.
func Read(path string) (Meta, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return Meta{}, nil
	}
	out, err := exec.Command(
		"ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path,
	).Output()
	if err != nil {
		return Meta{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseFFProbeOutput(out)
}

// Write updates metadata in a video file using ffmpeg with -codec copy (no re-encode).
// Returns nil if ffmpeg is not available — callers should log but not fail.
func Write(path string, u Updates) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	tmp, err := os.CreateTemp(dir, ".vm_tmp_*"+ext)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath) // no-op if Rename succeeds

	args := []string{"-i", path, "-codec", "copy", "-map_metadata", "0", "-y"}
	if u.Title != nil {
		args = append(args, "-metadata", "title="+*u.Title)
	}
	if u.Keywords != nil {
		args = append(args, "-metadata", "keywords="+strings.Join(u.Keywords, ","))
	}
	args = append(args, tmpPath)

	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, out)
	}
	return os.Rename(tmpPath, path)
}

// --- internal ---

type ffprobeOutput struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

func parseFFProbeOutput(data []byte) (Meta, error) {
	var result ffprobeOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return Meta{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	tags := result.Format.Tags
	m := Meta{
		Title:       tags["title"],
		Genre:       tags["genre"],
		Artist:      firstOf(tags, "artist", "album_artist"),
		Date:        firstOf(tags, "date", "year"),
		Comment:     tags["comment"],
		Description: firstOf(tags, "description", "desc"),
	}
	if kw := firstOf(tags, "keywords", "keyword"); kw != "" {
		for _, k := range strings.FieldsFunc(kw, func(r rune) bool {
			return r == ',' || r == ';'
		}) {
			if k = strings.TrimSpace(k); k != "" {
				m.Keywords = append(m.Keywords, k)
			}
		}
	}
	return m, nil
}

func firstOf(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := tags[k]; v != "" {
			return v
		}
	}
	return ""
}
