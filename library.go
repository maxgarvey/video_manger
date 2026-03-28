package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
	"github.com/maxgarvey/video_manger/transcode"
)

// retryBusy retries fn up to 3 times when SQLite returns SQLITE_BUSY,
// using a short exponential backoff.  Non-busy errors are returned immediately.
func retryBusy(fn func() error) error {
	for i := 0; i < 3; i++ {
		err := fn()
		if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") {
			return err
		}
		time.Sleep(time.Duration(100*(1<<i)) * time.Millisecond) // 100, 200, 400ms
	}
	return fn() // final attempt
}

// cleanShowName normalises a raw show string by replacing punctuation with
// spaces, collapsing whitespace, and trimming.
func cleanShowName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	// collapse multiple spaces
	return strings.Join(strings.Fields(s), " ")
}

// extractShowFromFilename attempts to parse a show name from a filename by
// looking for common season/episode patterns. The returned name is cleaned or
// empty if no pattern is recognised.
func extractShowFromFilename(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	// patterns capture the portion before the season/episode indicator
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(.+?)[. _\-]+S\d{1,2}E\d{1,2}`),
		regexp.MustCompile(`(?i)^(.+?)[. _\-]+Season[ ._\-]*\d{1,2}`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(base); m != nil {
			return cleanShowName(m[1])
		}
	}
	return ""
}

// inferShow attempts to determine a show/series name for a video based on
// its location within the registered root directory or from its filename.
// If the file lives inside a subdirectory of the root, the first path
// component is treated as the show name. Otherwise we fall back to parsing
// common season/episode patterns from the filename.
func inferShow(root, dir, filename string) string {
	rel, err := filepath.Rel(root, dir)
	if err == nil && rel != "." {
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) > 0 && parts[0] != "" {
			return cleanShowName(parts[0])
		}
	}
	return extractShowFromFilename(filename)
}

// containsWord checks if a string contains a word (whole word, case-insensitive).
// Words are sequences of letters/digits separated by non-alphanumeric chars.
func containsWord(s, word string) bool {
	lower := strings.ToLower(s)
	lowerWord := strings.ToLower(word)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for _, w := range words {
		if w == lowerWord {
			return true
		}
	}
	return false
}

// inferVideoType attempts to determine the type of video (TV, Movie, Concert, etc)
// based on metadata available at sync time.  Priority: explicit metadata via
// season/episode > tags > filename patterns.
func inferVideoType(filename string, seasonNum, _ int, tags []string) string {
	if seasonNum > 0 {
		return "TV"
	}
	for _, tag := range tags {
		if containsWord(tag, "youtube") || containsWord(tag, "channel") {
			return "YouTube"
		}
	}
	base := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if strings.Contains(base, "concert") || strings.Contains(base, "live performance") {
		return "Concert"
	}
	if strings.Contains(base, "vlog") || strings.Contains(base, "vlogging") {
		return "Vlog"
	}
	if strings.Contains(base, "blog") || strings.Contains(base, "blogging") {
		return "Blog"
	}
	return "Movie"
}

// syncDir walks a directory tree recursively and upserts all video files into
// the store. Subdirectories are not registered as separate directory entries;
// all videos under the tree share the same directory_id but store their actual
// containing subdirectory path so FilePath() resolves correctly.
// If ffprobe is available, native title is read and used to pre-populate
// display_name for videos that don't yet have one set.
func (s *server) syncDir(d store.Directory) {
	// Build a set of other registered directory paths so we don't walk into
	// them when d is a parent directory. That would incorrectly reassign
	// directory_id for videos that belong to a registered child directory.
	otherDirs := make(map[string]bool)
	if allDirs, err := s.store.ListDirectories(context.Background()); err == nil {
		for _, rd := range allDirs {
			if rd.ID != d.ID {
				otherDirs[filepath.Clean(rd.Path)] = true
			}
		}
	}

	if err := filepath.WalkDir(d.Path, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("sync walk error", "path", path, "err", err)
			return nil // keep walking
		}
		if de.IsDir() {
			// Skip subdirectories that are themselves registered directories.
			if path != d.Path && otherDirs[filepath.Clean(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !isVideoFile(de.Name()) {
			return nil
		}
		dir := filepath.Dir(path)
		var v store.Video
		if err := retryBusy(func() error {
			var e error
			v, e = s.store.UpsertVideo(context.Background(), d.ID, dir, de.Name())
			return e
		}); err != nil {
			slog.Warn("upsert video failed", "path", path, "err", err)
			return nil
		}
		// infer show name if not already set
		if v.ShowName == "" {
			show := inferShow(d.Path, dir, de.Name())
			if show != "" {
				if err := retryBusy(func() error {
					return s.store.UpdateVideoShowName(context.Background(), v.ID, show)
				}); err != nil {
					slog.Warn("set show name failed", "path", path, "err", err)
				}
				// update our local copy for later checks (e.g. thumbnail)
				v.ShowName = show
			}
		}
		if v.DisplayName == "" {
			if meta, err := metadata.Read(path); err == nil && meta.Title != "" {
				if err := retryBusy(func() error {
					return s.store.UpdateVideoName(context.Background(), v.ID, meta.Title)
				}); err != nil {
					slog.Warn("set native title failed", "path", path, "err", err)
				}
			}
		}
		if v.DurationS == 0 {
			if d := metadata.ReadDuration(path); d > 0 {
				if err := retryBusy(func() error {
					return s.store.UpdateVideoDuration(context.Background(), v.ID, d)
				}); err != nil {
					slog.Warn("set duration failed", "path", path, "err", err)
				}
			}
		}
		// Infer video type if not already set
		if v.VideoType == "" {
			tags, err := s.store.ListTagsByVideo(context.Background(), v.ID)
			if err != nil {
				slog.Warn("list tags for inference failed", "videoID", v.ID, "err", err)
			}
			var names []string
			for _, t := range tags {
				names = append(names, t.Name)
			}
			inferred := inferVideoType(de.Name(), v.SeasonNumber, v.EpisodeNumber, names)
			if err := retryBusy(func() error {
				return s.store.UpdateVideoType(context.Background(), v.ID, inferred)
			}); err != nil {
				slog.Warn("set video type failed", "path", path, "err", err)
			}
		}
		// Auto-tag with the registered directory's base name.
		var dirTag store.Tag
		if err := retryBusy(func() error {
			var e error
			dirTag, e = s.store.UpsertTag(context.Background(), filepath.Base(d.Path))
			return e
		}); err != nil {
			slog.Warn("upsert dir tag failed", "dir", d.Path, "err", err)
		} else if err := retryBusy(func() error {
			return s.store.TagVideo(context.Background(), v.ID, dirTag.ID)
		}); err != nil {
			slog.Warn("tag video with dir tag failed", "videoID", v.ID, "err", err)
		}
		// Apply optional JSON sidecar (same basename, .json extension).
		s.applySidecar(context.Background(), v)

		// Generate thumbnail if it doesn't exist and ffmpeg is available
		if v.ThumbnailPath == "" {
			thumbPath := filepath.Join(dir, strings.TrimSuffix(de.Name(), filepath.Ext(de.Name()))+"_thumb.jpg")
			if _, err := os.Stat(thumbPath); os.IsNotExist(err) {
				// Generate at random position
				position := 0.1 + rand.Float64()*0.8
				if err := transcode.GenerateThumbnail(path, thumbPath, position); err != nil {
					slog.Debug("auto thumbnail generation failed", "path", path, "err", err)
				} else {
					if err := retryBusy(func() error {
						return s.store.UpdateVideoThumbnail(context.Background(), v.ID, thumbPath)
					}); err != nil {
						slog.Warn("update thumbnail path failed", "videoID", v.ID, "err", err)
					}
				}
			} else if err == nil {
				// Thumbnail exists, update DB
				if err := retryBusy(func() error {
					return s.store.UpdateVideoThumbnail(context.Background(), v.ID, thumbPath)
				}); err != nil {
					slog.Warn("update existing thumbnail path failed", "videoID", v.ID, "err", err)
				}
			}
		}

		return nil
	}); err != nil {
		slog.Error("syncDir walk failed", "path", d.Path, "err", err)
	}

	// Prune DB records for files that no longer exist on disk.
	existing, err := s.store.ListVideosByDirectory(context.Background(), d.ID)
	if err != nil {
		slog.Error("syncDir list videos failed", "path", d.Path, "err", err)
		return
	}
	for _, v := range existing {
		if _, err := os.Stat(v.FilePath()); os.IsNotExist(err) {
			slog.Info("syncDir: removing stale entry", "path", v.FilePath())
			if err := retryBusy(func() error {
				return s.store.DeleteVideo(context.Background(), v.ID)
			}); err != nil {
				slog.Error("syncDir: delete stale video failed", "videoID", v.ID, "err", err)
			}
		}
	}
}

// startSyncDir marks a directory as syncing and runs syncDir in the background.
func (s *server) startSyncDir(d store.Directory) {
	s.syncingMu.Lock()
	s.syncingDirs[d.ID] = struct{}{}
	s.syncingMu.Unlock()
	go func() {
		s.syncDir(d)
		s.syncingMu.Lock()
		delete(s.syncingDirs, d.ID)
		s.syncingMu.Unlock()
	}()
}

// startLibraryPoller runs in the background, re-scanning all registered
// directories every 60 s so newly added files are picked up automatically.
// Directories are synced sequentially to avoid concurrent write contention
// on the single-writer SQLite database.
func (s *server) startLibraryPoller(ctx context.Context) {
	ticker := time.NewTicker(libraryPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dirs, err := s.store.ListDirectories(ctx)
			if err != nil {
				slog.Error("library poll: list dirs failed", "err", err)
				continue
			}
			// Sync sequentially in a single goroutine to reduce DB
			// write contention that blocks user-facing read queries.
			go func() {
				for _, d := range dirs {
					s.syncingMu.Lock()
					_, already := s.syncingDirs[d.ID]
					if already {
						s.syncingMu.Unlock()
						continue
					}
					s.syncingDirs[d.ID] = struct{}{}
					s.syncingMu.Unlock()

					start := time.Now()
					slog.Info("syncDir: start", "path", d.Path)
					s.syncDir(d)
					slog.Info("syncDir: done", "path", d.Path, "elapsed", time.Since(start).Round(time.Millisecond))

					s.syncingMu.Lock()
					delete(s.syncingDirs, d.ID)
					s.syncingMu.Unlock()
				}
			}()
		}
	}
}

// syncTagsToFile writes the current DB tags for a video back to the file as keywords.
func (s *server) syncTagsToFile(ctx context.Context, video store.Video) {
	tags, err := s.store.ListTagsByVideo(ctx, video.ID)
	if err != nil {
		slog.Warn("syncTagsToFile: list tags failed", "videoID", video.ID, "err", err)
		return
	}
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	if err := metadata.Write(video.FilePath(), metadata.Updates{Keywords: names}); err != nil {
		slog.Warn("syncTagsToFile: write failed", "path", video.FilePath(), "err", err)
	}
}

// ── Sidecar JSON ──────────────────────────────────────────────────────────────

// sidecarFieldMaxLen is the maximum byte length for any string field read from
// a sidecar JSON file.  Values longer than this are silently truncated before
// being stored so that a malicious sidecar cannot fill the database with
// arbitrarily large strings.
const sidecarFieldMaxLen = 1024

// clampStr truncates s to sidecarFieldMaxLen bytes if it is longer.
func clampStr(s string) string {
	if len(s) > sidecarFieldMaxLen {
		return s[:sidecarFieldMaxLen]
	}
	return s
}

// sidecarData holds optional metadata loaded from a JSON file that sits next
// to a video file (same basename, .json extension).
// Example: "film.mp4" → "film.json"
//
// Only fields that are present and non-zero in the JSON are applied; absent
// fields leave the existing DB values untouched.
type sidecarData struct {
	Title         string   `json:"title"`
	Tags          []string `json:"tags"`
	Actors        string   `json:"actors"`
	Genre         string   `json:"genre"`
	SeasonNumber  int      `json:"season"`
	EpisodeNumber int      `json:"episode"`
	EpisodeTitle  string   `json:"episode_title"`
	Studio        string   `json:"studio"`
	Channel       string   `json:"channel"`
}

// readSidecar looks for a <basename>.json file alongside videoPath.
// Returns (data, true, nil) when found and valid, (_, false, nil) when absent,
// and (_, false, err) when present but malformed.
func readSidecar(videoPath string) (sidecarData, bool, error) {
	ext := filepath.Ext(videoPath)
	sidecarPath := videoPath[:len(videoPath)-len(ext)] + ".json"
	raw, err := os.ReadFile(sidecarPath)
	if os.IsNotExist(err) {
		return sidecarData{}, false, nil
	}
	if err != nil {
		return sidecarData{}, false, err
	}
	var sc sidecarData
	if err := json.Unmarshal(raw, &sc); err != nil {
		return sidecarData{}, false, fmt.Errorf("parse sidecar %s: %w", sidecarPath, err)
	}
	return sc, true, nil
}

// applySidecar reads the JSON sidecar for v.FilePath() (if present) and
// updates the video's title, fields, and tags in the store.
// Fields absent from the sidecar are left unchanged.
func (s *server) applySidecar(ctx context.Context, v store.Video) {
	sc, ok, err := readSidecar(v.FilePath())
	if err != nil {
		slog.Warn("sidecar parse failed", "path", v.FilePath(), "err", err)
		return
	}
	if !ok {
		return
	}

	// Clamp all string fields to sidecarFieldMaxLen to prevent oversized DB writes.
	title := clampStr(sc.Title)
	actors := clampStr(sc.Actors)
	genre := clampStr(sc.Genre)
	episodeTitle := clampStr(sc.EpisodeTitle)
	studio := clampStr(sc.Studio)
	channel := clampStr(sc.Channel)

	if title != "" {
		if err := s.store.UpdateVideoName(ctx, v.ID, title); err != nil {
			slog.Warn("sidecar: update title failed", "videoID", v.ID, "err", err)
		}
	}

	// Only write video fields when the sidecar supplies at least one value,
	// so that an empty sidecar never clears fields set through the UI.
	if actors != "" || genre != "" || sc.SeasonNumber > 0 ||
		sc.EpisodeNumber > 0 || episodeTitle != "" ||
		studio != "" || channel != "" {
		f := store.VideoFields{
			Genre:         genre,
			SeasonNumber:  sc.SeasonNumber,
			EpisodeNumber: sc.EpisodeNumber,
			EpisodeTitle:  episodeTitle,
			Actors:        actors,
			Studio:        studio,
			Channel:       channel,
		}
		if err := s.store.UpdateVideoFields(ctx, v.ID, f); err != nil {
			slog.Warn("sidecar: update fields failed", "videoID", v.ID, "err", err)
		}
	}

	for _, tagName := range sc.Tags {
		if tagName = clampStr(strings.TrimSpace(tagName)); tagName == "" {
			continue
		}
		tag, err := s.store.UpsertTag(ctx, tagName)
		if err != nil {
			slog.Warn("sidecar: upsert tag failed", "tag", tagName, "err", err)
			continue
		}
		if err := s.store.TagVideo(ctx, v.ID, tag.ID); err != nil {
			slog.Warn("sidecar: tag video failed", "tag", tagName, "videoID", v.ID, "err", err)
		}
	}
}

// isVideoFile reports whether name has a video file extension.
func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".ogg", ".mov", ".mkv", ".avi",
		".flv", ".wmv", ".m4v", ".ts", ".m2ts", ".vob",
		".ogv", ".3gp", ".mpeg", ".mpg", ".divx", ".xvid":
		return true
	}
	return false
}

// checkBinaries warns on startup if any optional external tool is missing.
// The server starts regardless; affected endpoints will return 500 when invoked.
func checkBinaries() {
	for _, bin := range []string{"ffmpeg", "ffprobe", "yt-dlp"} {
		if _, err := exec.LookPath(bin); err != nil {
			slog.Warn("binary not found in PATH", "binary", bin)
		}
	}
}
