// handlers_videos.go – video playback, listing, tags, rating, progress,
// file operations (copy/move/relocate), share panel, and duplicate detection.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
	"github.com/maxgarvey/video_manger/transcode"
)

// ── General / page-level handlers ───────────────────────────────────────────

func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	addrs := localAddresses(s.port)
	mdns := ""
	if s.mdnsName != "" {
		mdns = "http://" + s.mdnsName + ":" + s.port
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"port":      s.port,
		"addresses": addrs,
		"mdns":      mdns,
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	rokuEnabled, _ := s.store.GetSetting(r.Context(), "roku_enabled")
	render(w, "index.html", struct {
		RokuEnabled bool
	}{
		RokuEnabled: rokuEnabled == "true",
	})
}

func (s *server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), video.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allTags, err := s.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, statErr := os.Stat(video.FilePath())
	fileNotFound := statErr != nil

	srtPath := strings.TrimSuffix(video.FilePath(), filepath.Ext(video.FilePath())) + ".srt"
	_, srtErr := os.Stat(srtPath)
	hasSubtitles := srtErr == nil

	libPath, _ := s.store.GetSetting(r.Context(), "library_path")
	data := struct {
		Video        store.Video
		Tags         []store.Tag
		AllTags      []store.Tag
		FileNotFound bool
		HasSubtitles bool
		LibraryPath  string
		Formats      []transcode.FormatEntry
	}{video, tags, allTags, fileNotFound, hasSubtitles, strings.TrimSpace(libPath), transcode.FormatList}
	render(w, "player.html", data)
}

// handleServeSubtitles converts a sidecar .srt file to WebVTT on-the-fly and
// serves it so the browser <track> element can consume it directly.
func (s *server) handleServeSubtitles(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	srtPath := strings.TrimSuffix(video.FilePath(), filepath.Ext(video.FilePath())) + ".srt"
	data, err := os.ReadFile(srtPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	vtt := srtToWebVTT(string(data))
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	fmt.Fprint(w, vtt)
}

// srtToWebVTT converts SRT subtitle text to WebVTT format.
// The only structural differences are the header and timestamp separators:
// SRT uses commas for milliseconds (00:00:01,000) while WebVTT uses dots
// (00:00:01.000).
func srtToWebVTT(srt string) string {
	lines := strings.Split(srt, "\n")
	out := make([]string, 0, len(lines)+2)
	out = append(out, "WEBVTT", "")
	for _, line := range lines {
		// Replace timestamp separators: "HH:MM:SS,mmm --> HH:MM:SS,mmm"
		if strings.Contains(line, " --> ") {
			line = strings.ReplaceAll(line, ",", ".")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func (s *server) handleVideoFile(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	http.ServeFile(w, r, video.FilePath())
}

// ── Video: name, tags, rating, delete, relocate ──────────────────────────────

func (s *server) handleUpdateVideoName(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	name := r.FormValue("name")
	if err := s.store.UpdateVideoName(r.Context(), id, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if name != "" {
		if err := metadata.Write(video.FilePath(), metadata.Updates{Title: &name}); err != nil {
			slog.Warn("write title metadata failed", "path", video.FilePath(), "err", err)
		}
	}
	// Trigger a video-list refresh so the sidebar reflects the new name immediately,
	// and a metadata panel refresh for the player page.
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"videoRenamed":true,"videoLabelled":{"id":%d}}`, id))
	w.Write([]byte(html.EscapeString(video.Title()))) //nolint:errcheck
}

func (s *server) handleVideoTags(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "video_tags.html", videoTagsData{id, tags})
}

func (s *server) handleAddVideoTag(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	tagName := strings.TrimSpace(r.FormValue("tag"))
	if tagName == "" {
		http.Error(w, "tag name required", http.StatusBadRequest)
		return
	}
	reservedPrefixes := []string{"show:", "type:", "genre:", "actor:", "studio:", "channel:"}
	for _, p := range reservedPrefixes {
		if strings.HasPrefix(strings.ToLower(tagName), p) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<p style="font-size:0.82rem;color:#f87">Use the dedicated field to set %s</p>`, html.EscapeString(strings.TrimSuffix(p, ":")))
			return
		}
	}
	tag, err := s.store.UpsertTag(r.Context(), tagName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.TagVideo(r.Context(), id, tag.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshVideoTags(w, r, id)
}

func (s *server) handleRemoveVideoTag(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	tagID, err := strconv.ParseInt(chi.URLParam(r, "tagID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid tag id", http.StatusBadRequest)
		return
	}
	if err := s.store.UntagVideo(r.Context(), id, tagID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.PruneOrphanTags(r.Context()); err != nil {
		slog.Warn("prune orphan tags failed", "err", err)
	}
	s.refreshVideoTags(w, r, id)
}

func (s *server) handleVideoDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	render(w, "video_delete_confirm.html", video)
}

func (s *server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	s.deleteVideoAndRefresh(w, r, id)
}

func (s *server) handleDeleteVideoAndFile(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	if err := os.Remove(video.FilePath()); err != nil {
		slog.Warn("delete file failed", "path", video.FilePath(), "err", err)
	}
	s.deleteVideoAndRefresh(w, r, video.ID)
}

func (s *server) handleRelocateVideo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	newPath := r.FormValue("newpath")
	if newPath == "" {
		http.Error(w, "newpath required", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(newPath); err != nil {
		http.Error(w, "file not accessible at new path", http.StatusBadRequest)
		return
	}
	newDir := filepath.Dir(newPath)
	newFilename := filepath.Base(newPath)

	// Restrict relocation to paths under a registered directory for security.
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dirID, underLib := findRegisteredDir(dirs, newDir)
	if !underLib {
		http.Error(w, "new path must be inside a registered library directory", http.StatusForbidden)
		return
	}
	if dirID == 0 {
		// newDir is a sub-folder of a registered directory; register it so the
		// video is tracked under its own directory entry.
		dir, err := s.store.AddDirectory(r.Context(), newDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dirID = dir.ID
	}

	if err := s.store.UpdateVideoPath(r.Context(), id, dirID, newDir, newFilename); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.handlePlayer(w, r)
}

// ── Video list ────────────────────────────────────────────────────────────────

// filterVideos applies optional type and rating filters from q in-place.
func filterVideos(videos []store.Video, q url.Values) []store.Video {
	if typeVal := q.Get("type"); typeVal != "" {
		filtered := videos[:0]
		for _, v := range videos {
			if v.VideoType == typeVal {
				filtered = append(filtered, v)
			}
		}
		videos = filtered
	}
	if q.Get("rating") != "" {
		minRating, _ := strconv.Atoi(q.Get("rating"))
		if minRating < 1 {
			minRating = 1
		}
		filtered := videos[:0]
		for _, v := range videos {
			if v.Rating >= minRating {
				filtered = append(filtered, v)
			}
		}
		videos = filtered
	}
	return videos
}

// serveVideoList renders the video list, respecting tag_id, q, and the
// video_sort setting.
func (s *server) serveVideoList(w http.ResponseWriter, r *http.Request) {
	var (
		videos []store.Video
		err    error
	)
	q := r.URL.Query()
	sortOrder, _ := s.store.GetSetting(r.Context(), "video_sort")
	if q.Get("q") != "" {
		videos, err = s.store.SearchVideos(r.Context(), q.Get("q"))
	} else {
		if q.Get("tag_id") != "" {
			tagID, _ := strconv.ParseInt(q.Get("tag_id"), 10, 64)
			videos, err = s.store.ListVideosByTag(r.Context(), tagID)
		} else if sortOrder == "rating" {
			videos, err = s.store.ListVideosByRating(r.Context())
		} else {
			videos, err = s.store.ListVideos(r.Context())
		}
		if err == nil {
			videos = filterVideos(videos, q)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// SQL ORDER BY already returns videos in the correct order; no Go-level sort needed.
	// Pagination: default 500 per page; page= is 1-indexed.
	const defaultPageSize = 500
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = defaultPageSize
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	total := len(videos)
	start := min((page-1)*limit, total)
	end := min(start+limit, total)
	pageVideos := videos[start:end]

	// WatchedAt is embedded in each Video via SQL LEFT JOIN; no separate query needed.
	data := struct {
		Groups   []videoGroup
		Page     int
		PageSize int
		Total    int
	}{groupVideosByShowSeason(pageVideos), page, limit, total}
	render(w, "video_list.html", data)
}

// ── Watch history / progress ──────────────────────────────────────────────────

func (s *server) handlePostProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	pos, _ := strconv.ParseFloat(r.FormValue("position"), 64)
	if err := s.store.RecordWatch(r.Context(), id, pos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	rec, err := s.store.GetWatch(r.Context(), id)
	if err != nil {
		// Not yet watched — return zero position.
		json.NewEncoder(w).Encode(map[string]any{"position": 0, "watched_at": ""}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"position":   rec.Position,
		"watched_at": rec.WatchedAt,
	})
}

// handleMarkWatched manually marks a video as watched and refreshes the
// video list so the ✓ indicator updates immediately.
func (s *server) handleMarkWatched(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := s.store.RecordWatch(r.Context(), id, 1); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.serveVideoList(w, r)
}

func (s *server) handleClearProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := s.store.ClearWatch(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.serveVideoList(w, r)
}

// ── File operations: copy, move ───────────────────────────────────────────────

func (s *server) handleCopyToLibrary(w http.ResponseWriter, r *http.Request) {
	libPath, _ := s.store.GetSetting(r.Context(), "library_path")
	libPath = strings.TrimSpace(libPath)
	if libPath == "" {
		http.Error(w, "Library path not configured — set it in Settings.", http.StatusBadRequest)
		return
	}
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	src := video.FilePath()
	if _, err := os.Stat(src); err != nil {
		http.Error(w, "source file not found", http.StatusNotFound)
		return
	}
	if err := os.MkdirAll(libPath, 0755); err != nil {
		http.Error(w, "cannot create library directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	dstName := freeOutputName(libPath, stem, "", ext)
	dst := filepath.Join(libPath, dstName)
	if err := copyFile(src, dst); err != nil {
		http.Error(w, "copy failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `<span style="color:#4a9a4a;font-size:0.8rem">✓ Copied to %s</span>`, dstName)
}

// handleRenameVideo renames a video file on disk and updates the DB.
// Form field: name=<new_filename_with_extension>
func (s *server) handleRenameVideo(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	newName := strings.TrimSpace(r.FormValue("name"))
	if newName == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(newName, "/\\") || strings.Contains(newName, "..") {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	if newName == video.Filename {
		s.serveVideoList(w, r)
		return
	}
	src := video.FilePath()
	dst := filepath.Join(video.DirectoryPath, newName)
	if err := os.Rename(src, dst); err != nil {
		http.Error(w, "rename failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.UpdateVideoPath(r.Context(), video.ID, video.DirectoryID, video.DirectoryPath, newName); err != nil {
		// Best-effort rollback.
		_ = os.Rename(dst, src)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.serveVideoList(w, r)
}

// resolveDestDir returns the destination path and directory ID for a move,
// creating and registering a sub-folder when subdir is non-empty.
func (s *server) resolveDestDir(ctx context.Context, targetDir store.Directory, subdir string) (path string, id int64, err error) {
	if subdir == "" {
		return targetDir.Path, targetDir.ID, nil
	}
	destPath := filepath.Join(targetDir.Path, subdir)
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return "", 0, fmt.Errorf("could not create sub-folder: %w", err)
	}
	newDir, err := s.store.AddDirectory(ctx, destPath)
	if err != nil {
		// Already registered — find the existing record.
		dirs, listErr := s.store.ListDirectories(ctx)
		if listErr != nil {
			return "", 0, fmt.Errorf("failed to register sub-folder: %w", err)
		}
		for _, d := range dirs {
			if d.Path == destPath {
				newDir = d
				break
			}
		}
	}
	return destPath, newDir.ID, nil
}

// moveFile moves src to dst. It tries os.Rename first and falls back to a
// copy+delete for cross-device moves. Returns crossDevice=true when the
// fallback was used (the source still exists until the caller removes it).
func moveFile(src, dst string) (crossDevice bool, err error) {
	if err := os.Rename(src, dst); err == nil {
		return false, nil
	}
	if err := copyFile(src, dst); err != nil {
		return true, err
	}
	return true, nil
}

// moveVideoThumbnail moves video's thumbnail file to destDirPath and updates
// the DB. Best-effort: logs on failure but does not abort.
func (s *server) moveVideoThumbnail(ctx context.Context, video store.Video, destDirPath string) {
	if video.ThumbnailPath == "" {
		return
	}
	thumbDst := filepath.Join(destDirPath, filepath.Base(video.ThumbnailPath))
	if err := os.Rename(video.ThumbnailPath, thumbDst); err != nil {
		if err2 := copyFile(video.ThumbnailPath, thumbDst); err2 != nil {
			slog.Warn("could not move thumbnail", "src", video.ThumbnailPath, "dst", thumbDst, "err", err2)
			return
		}
		_ = os.Remove(video.ThumbnailPath)
	}
	if err := s.store.UpdateVideoThumbnail(ctx, video.ID, thumbDst); err != nil {
		slog.Warn("thumbnail moved on disk but DB update failed", "dst", thumbDst, "err", err)
	}
}

// handleMoveVideo moves a video file to a different registered directory.
// Optional form field "subdir" creates a sub-folder inside the target dir.
func (s *server) handleMoveVideo(w http.ResponseWriter, r *http.Request) {
	dirIDStr := strings.TrimSpace(r.FormValue("dir_id"))
	subdir := strings.TrimSpace(r.FormValue("subdir"))

	dirID, err := strconv.ParseInt(dirIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid dir_id", http.StatusBadRequest)
		return
	}
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	targetDir, err := s.store.GetDirectory(r.Context(), dirID)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	// Reject traversal in subdir before any filesystem work.
	if subdir != "" && (strings.ContainsAny(subdir, "/\\") || strings.Contains(subdir, "..")) {
		http.Error(w, "subdir must not contain path separators or '..'", http.StatusBadRequest)
		return
	}

	destDirPath, destDirID, err := s.resolveDestDir(r.Context(), targetDir, subdir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	src := video.FilePath()
	dst := filepath.Join(destDirPath, video.Filename)
	if src == dst {
		http.Error(w, "source and destination are the same", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(dst); err == nil {
		http.Error(w, "a file with that name already exists in the destination", http.StatusConflict)
		return
	}

	crossDevice, err := moveFile(src, dst)
	if err != nil {
		http.Error(w, "move failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.store.UpdateVideoPath(r.Context(), video.ID, destDirID, destDirPath, video.Filename); err != nil {
		// DB update failed — roll back the filesystem change for consistency.
		if crossDevice {
			if rb := os.Remove(dst); rb != nil {
				slog.Error("move rollback failed: copy is at dst but DB was not updated",
					"src", src, "dst", dst, "dbErr", err, "rbErr", rb)
			}
		} else {
			if rb := os.Rename(dst, src); rb != nil {
				slog.Error("move rollback failed", "src", src, "dst", dst, "dbErr", err, "rbErr", rb)
			}
		}
		http.Error(w, "move failed (database update): "+err.Error(), http.StatusInternalServerError)
		return
	}

	if crossDevice {
		if err := os.Remove(src); err != nil {
			slog.Warn("cross-device move: could not remove source after successful DB update",
				"src", src, "err", err)
		}
	}

	s.moveVideoThumbnail(r.Context(), video, destDirPath)

	// Sync both directories so the library reflects the change.
	s.startSyncDir(targetDir)
	if video.DirectoryID != 0 && video.DirectoryID != targetDir.ID {
		if oldDir, err := s.store.GetDirectory(r.Context(), video.DirectoryID); err == nil {
			s.startSyncDir(oldDir)
		}
	}
	s.serveVideoList(w, r)
}

// ── Bulk move ────────────────────────────────────────────────────────────────

func (s *server) handleBulkMoveVideos(w http.ResponseWriter, r *http.Request) {
	dirIDStr := strings.TrimSpace(r.FormValue("dir_id"))
	idsStr := strings.TrimSpace(r.FormValue("video_ids"))

	dirID, err := strconv.ParseInt(dirIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid dir_id", http.StatusBadRequest)
		return
	}
	if idsStr == "" {
		http.Error(w, "no video_ids provided", http.StatusBadRequest)
		return
	}

	targetDir, err := s.store.GetDirectory(r.Context(), dirID)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	destDirPath, destDirID, err := s.resolveDestDir(r.Context(), targetDir, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ids := strings.Split(idsStr, ",")
	jobID := newToken()
	job := &bulkMoveJob{
		ch:    make(chan string, 4096),
		total: len(ids),
	}
	s.moveJobsMu.Lock()
	s.moveJobs[jobID] = job
	s.moveJobsMu.Unlock()

	go s.runBulkMove(job, ids, targetDir, destDirPath, destDirID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"job_id": jobID}) //nolint:errcheck
}

// runBulkMove performs the actual file moves in a background goroutine,
// sending progress lines to job.ch.
func (s *server) runBulkMove(job *bulkMoveJob, idStrs []string, targetDir store.Directory, destDirPath string, destDirID int64) {
	defer scheduleJobCleanup(job.ch, func() {
		// No need to delete — job will be GC'd after channel consumers are done.
	})

	ctx := context.Background()
	sourceDirs := map[int64]struct{}{}

	for i, idStr := range idStrs {
		id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if err != nil {
			job.fails++
			job.ch <- fmt.Sprintf("Error: %s: invalid id", idStr)
			continue
		}
		video, err := s.store.GetVideo(ctx, id)
		if err != nil {
			job.fails++
			job.ch <- fmt.Sprintf("Error: %s: not found", idStr)
			continue
		}

		job.ch <- fmt.Sprintf("Moving %d/%d: %s", i+1, job.total, video.Filename)

		src := video.FilePath()
		dst := filepath.Join(destDirPath, video.Filename)
		if src == dst {
			continue // already in target
		}
		if _, err := os.Stat(dst); err == nil {
			job.fails++
			job.ch <- fmt.Sprintf("Error: %s: already exists in destination", video.Filename)
			continue
		}

		crossDevice, err := moveFile(src, dst)
		if err != nil {
			job.fails++
			job.ch <- fmt.Sprintf("Error: %s: %s", video.Filename, err.Error())
			continue
		}

		if err := s.store.UpdateVideoPath(ctx, video.ID, destDirID, destDirPath, video.Filename); err != nil {
			if crossDevice {
				os.Remove(dst) //nolint:errcheck
			} else {
				os.Rename(dst, src) //nolint:errcheck
			}
			job.fails++
			job.ch <- fmt.Sprintf("Error: %s: db update failed", video.Filename)
			continue
		}

		if crossDevice {
			if err := os.Remove(src); err != nil {
				slog.Warn("bulk move: could not remove source", "src", src, "err", err)
			}
		}

		s.moveVideoThumbnail(ctx, video, destDirPath)
		job.moved++
		if video.DirectoryID != 0 {
			sourceDirs[video.DirectoryID] = struct{}{}
		}
	}

	// Sync directories once at the end.
	s.startSyncDir(targetDir)
	for srcDirID := range sourceDirs {
		if srcDirID != targetDir.ID {
			if d, err := s.store.GetDirectory(ctx, srcDirID); err == nil {
				s.startSyncDir(d)
			}
		}
	}
}

// handleBulkMoveEvents streams bulk-move progress as Server-Sent Events.
func (s *server) handleBulkMoveEvents(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	s.moveJobsMu.Lock()
	job, ok := s.moveJobs[jobID]
	s.moveJobsMu.Unlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}

	ctx := r.Context()
loop:
	for {
		select {
		case line, open := <-job.ch:
			if !open {
				break loop
			}
			sse.Data(line)
		case <-ctx.Done():
			return
		}
	}

	if job.err != nil {
		sse.Event("error", job.err.Error())
	} else {
		data, _ := json.Marshal(map[string]any{
			"moved": job.moved,
			"fails": job.fails,
			"total": job.total,
		})
		sse.Event("done", string(data))
	}
}

// ── Rating ────────────────────────────────────────────────────────────────────

func (s *server) handleSetRating(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	rating, _ := strconv.Atoi(r.FormValue("rating"))
	if rating < 0 || rating > 2 {
		http.Error(w, "rating must be 0, 1, or 2", http.StatusBadRequest)
		return
	}
	if err := s.store.SetVideoRating(r.Context(), video.ID, rating); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := s.store.GetVideo(r.Context(), video.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "rating_buttons.html", updated)
}

// ── Video Type ────────────────────────────────────────────────────────────────

func (s *server) handleSetVideoType(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	videoType := strings.TrimSpace(r.FormValue("type"))
	if !store.IsValidVideoType(videoType) {
		http.Error(w, "invalid type", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateVideoType(r.Context(), video.ID, videoType); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := s.store.GetVideo(r.Context(), video.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"videoLabelled":{"id":%d}}`, video.ID))
	render(w, "video_type_badge.html", updated)
}

// ── Color Label ───────────────────────────────────────────────────────────────

func (s *server) handleSetVideoColor(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	color := strings.TrimSpace(r.FormValue("color"))
	if !store.IsValidColorLabel(color) {
		http.Error(w, "invalid color", http.StatusBadRequest)
		return
	}
	if err := s.store.SetExclusiveSystemTag(r.Context(), video.ID, "color", color); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := s.store.GetVideo(r.Context(), video.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "color_label.html", updated)
}

// ── Share panel ───────────────────────────────────────────────────────────────

func (s *server) handleSharePanel(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	suffix := fmt.Sprintf("/video/%d", video.ID)
	addrs := localAddresses(s.port)
	links := make([]string, 0, len(addrs)+1)
	if s.mdnsName != "" {
		links = append(links, "http://"+s.mdnsName+":"+s.port+suffix)
	}
	for _, a := range addrs {
		links = append(links, a+suffix)
	}
	render(w, "share_panel.html", struct {
		VideoID int64
		Links   []string
	}{video.ID, links})
}

// ── Duplicates / utility ──────────────────────────────────────────────────────

// dupGroup holds a set of videos that appear to be duplicates (same filename + size).
type dupGroup struct {
	Filename string
	SizeMB   string
	Videos   []store.Video
}

func (s *server) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	videos, err := s.store.ListVideos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type key struct {
		name string
		size int64
	}
	buckets := map[key][]store.Video{}
	for _, v := range videos {
		info, err := os.Stat(v.FilePath())
		if err != nil {
			continue // file missing from disk; skip
		}
		k := key{v.Filename, info.Size()}
		buckets[k] = append(buckets[k], v)
	}

	var groups []dupGroup
	for k, vs := range buckets {
		if len(vs) < 2 {
			continue
		}
		sizeMB := fmt.Sprintf("%.1f MB", float64(k.size)/(1024*1024))
		groups = append(groups, dupGroup{Filename: k.name, SizeMB: sizeMB, Videos: vs})
	}

	render(w, "duplicates.html", groups)
}

func (s *server) handleNextUnwatched(w http.ResponseWriter, r *http.Request) {
	tagID, _ := strconv.ParseInt(r.URL.Query().Get("tag_id"), 10, 64)
	q := r.URL.Query().Get("q")

	var id int64
	var title string
	var err error

	nextFromSearch, _ := s.store.GetSetting(r.Context(), "next_from_search")
	if nextFromSearch == "true" && q != "" {
		id, title, err = s.store.GetNextUnwatchedFromSearchLite(r.Context(), q, tagID)
	} else {
		id, title, err = s.store.GetNextUnwatchedLite(r.Context(), tagID)
	}
	if err != nil {
		http.Error(w, "no unwatched videos", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "title": title}) //nolint:errcheck
}

func (s *server) handleRandomVideoID(w http.ResponseWriter, r *http.Request) {
	video, err := s.store.GetRandomVideo(r.Context())
	if err != nil {
		http.Error(w, "no videos", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": video.ID, "title": video.Title()}) //nolint:errcheck
}

// quickLabelData bundles video, current tags, and available directories for
// the quick-label modal (features 4 & 5: tag editing + move-to-folder).
type quickLabelData struct {
	Video store.Video
	Tags  []store.Tag
	Dirs  []store.Directory
}

func (s *server) handleQuickLabelModal(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), video.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "quick_label_modal.html", quickLabelData{Video: video, Tags: tags, Dirs: dirs})
}

func (s *server) handleQuickLabelSubmit(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name != "" {
		if err := s.store.UpdateVideoName(r.Context(), video.ID, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	fields := store.VideoFields{
		Genre:         r.FormValue("genre"),
		SeasonNumber:  parseInt(r.FormValue("season")),
		EpisodeNumber: parseInt(r.FormValue("episode")),
		EpisodeTitle:  r.FormValue("episode_title"),
		Actors:        r.FormValue("actors"),
		Studio:        r.FormValue("studio"),
		Channel:       r.FormValue("channel"),
		AirDate:       r.FormValue("air_date"),
	}
	if err := s.store.UpdateVideoFields(r.Context(), video.ID, fields); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"videoLabelled":{"id":%d}}`, video.ID))
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleGenerateThumbnail(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}

	// Use random position between 10% and 90% if not specified
	position := 0.1 + rand.Float64()*0.8 // 0.1 to 0.9

	if posStr := r.URL.Query().Get("position"); posStr != "" {
		if p, err := strconv.ParseFloat(posStr, 64); err == nil && p >= 0 && p <= 1 {
			position = p
		}
	}

	thumbPath := filepath.Join(
		filepath.Dir(video.FilePath()),
		strings.TrimSuffix(video.Filename, filepath.Ext(video.Filename))+"_thumb.jpg",
	)
	if err := transcode.GenerateThumbnail(video.FilePath(), thumbPath, position); err != nil {
		slog.Warn("generate thumbnail failed", "path", video.FilePath(), "err", err)
		http.Error(w, "failed to generate thumbnail", http.StatusInternalServerError)
		return
	}
	if err := s.store.UpdateVideoThumbnail(r.Context(), video.ID, thumbPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleServeThumbnail(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	if video.ThumbnailPath == "" {
		http.Error(w, "no thumbnail", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, video.ThumbnailPath)
}
