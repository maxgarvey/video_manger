package main

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

// syncDir walks a directory tree recursively and upserts all video files into
// the store. Subdirectories are not registered as separate directory entries;
// all videos under the tree share the same directory_id but store their actual
// containing subdirectory path so FilePath() resolves correctly.
// If ffprobe is available, native title is read and used to pre-populate
// display_name for videos that don't yet have one set.
func (s *server) syncDir(d store.Directory) {
	if err := filepath.WalkDir(d.Path, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("sync walk error", "path", path, "err", err)
			return nil // keep walking
		}
		if de.IsDir() || !isVideoFile(de.Name()) {
			return nil
		}
		dir := filepath.Dir(path)
		v, err := s.store.UpsertVideo(context.Background(), d.ID, dir, de.Name())
		if err != nil {
			slog.Warn("upsert video failed", "path", path, "err", err)
			return nil
		}
		if v.DisplayName == "" {
			if meta, err := metadata.Read(path); err == nil && meta.Title != "" {
				if err := s.store.UpdateVideoName(context.Background(), v.ID, meta.Title); err != nil {
					slog.Warn("set native title failed", "path", path, "err", err)
				}
			}
		}
		// Auto-tag with the registered directory's base name.
		dirTag, err := s.store.UpsertTag(context.Background(), filepath.Base(d.Path))
		if err != nil {
			slog.Warn("upsert dir tag failed", "dir", d.Path, "err", err)
		} else if err := s.store.TagVideo(context.Background(), v.ID, dirTag.ID); err != nil {
			slog.Warn("tag video with dir tag failed", "videoID", v.ID, "err", err)
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
			if err := s.store.DeleteVideo(context.Background(), v.ID); err != nil {
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
// Directories that are already being synced are skipped to avoid races.
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
			for _, d := range dirs {
				s.syncingMu.Lock()
				_, already := s.syncingDirs[d.ID]
				s.syncingMu.Unlock()
				if !already {
					s.startSyncDir(d)
				}
			}
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
