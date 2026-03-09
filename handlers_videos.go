// handlers_videos.go – video playback, listing, tags, rating, progress,
// file operations (copy/move/relocate), share panel, and duplicate detection.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math/rand/v2"
	"net/http"
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
	render(w, "index.html", nil)
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

// serveVideoList renders the video list, respecting tag_id, q, and the
// video_sort setting.
func (s *server) serveVideoList(w http.ResponseWriter, r *http.Request) {
	var (
		videos []store.Video
		err    error
	)
	q := r.URL.Query()
	sortOrder, _ := s.store.GetSetting(r.Context(), "video_sort")
	isSearch := q.Get("q") != ""
	if isSearch {
		videos, err = s.store.SearchVideos(r.Context(), q.Get("q"))
	} else {
		// start with either all videos or those matching a tag or rating sort
		if q.Get("tag_id") != "" {
			tagID, _ := strconv.ParseInt(q.Get("tag_id"), 10, 64)
			videos, err = s.store.ListVideosByTag(r.Context(), tagID)
		} else if sortOrder == "rating" {
			videos, err = s.store.ListVideosByRating(r.Context())
		} else {
			videos, err = s.store.ListVideos(r.Context())
		}
		if err == nil {
			// apply additional attr filters: type first, then rating threshold
			typeVal := q.Get("type")
			if typeVal != "" {
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
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
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

	destDirPath := targetDir.Path
	destDirID := targetDir.ID

	// Create sub-folder if requested.
	if subdir != "" {
		// Reject names with path separators or parent-dir references to
		// prevent traversal outside the target directory (e.g. "../../etc").
		if strings.ContainsAny(subdir, "/\\") || strings.Contains(subdir, "..") {
			http.Error(w, "subdir must not contain path separators or '..'", http.StatusBadRequest)
			return
		}
		destDirPath = filepath.Join(targetDir.Path, subdir)
		if err := os.MkdirAll(destDirPath, 0755); err != nil {
			http.Error(w, "could not create sub-folder: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Register the new sub-directory so it shows up in the library.
		newDir, err := s.store.AddDirectory(r.Context(), destDirPath)
		if err != nil {
			// Already registered — get the existing one.
			dirs, listErr := s.store.ListDirectories(r.Context())
			if listErr != nil {
				http.Error(w, "failed to register sub-folder: "+err.Error(), http.StatusInternalServerError)
				return
			}
			for _, d := range dirs {
				if d.Path == destDirPath {
					newDir = d
					break
				}
			}
		}
		destDirID = newDir.ID
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

	// Try a fast rename first; fall back to copy+remove for cross-device moves.
	crossDevice := false
	if err := os.Rename(src, dst); err != nil {
		crossDevice = true
		if err2 := copyFile(src, dst); err2 != nil {
			http.Error(w, "move failed: "+err2.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := s.store.UpdateVideoPath(r.Context(), video.ID, destDirID, destDirPath, video.Filename); err != nil {
		// DB update failed after the file has already been copied/moved.
		// Attempt to roll back the filesystem change so nothing is left inconsistent.
		if crossDevice {
			// For cross-device copies the source is still intact; just remove
			// the copy at dst.  os.Rename would fail with EXDEV here too.
			if rb := os.Remove(dst); rb != nil {
				slog.Error("move rollback failed: copy is at dst but DB was not updated",
					"src", src, "dst", dst, "dbErr", err, "rbErr", rb)
			}
		} else {
			// Same-device rename was atomic; move it back.
			if rb := os.Rename(dst, src); rb != nil {
				slog.Error("move rollback failed", "src", src, "dst", dst, "dbErr", err, "rbErr", rb)
			}
		}
		http.Error(w, "move failed (database update): "+err.Error(), http.StatusInternalServerError)
		return
	}

	// For cross-device moves, now that the DB is consistent, remove the source.
	if crossDevice {
		if err := os.Remove(src); err != nil {
			slog.Warn("cross-device move: could not remove source after successful DB update",
				"src", src, "err", err)
		}
	}

	// Sync both directories so the library reflects the change.
	s.startSyncDir(targetDir)
	if video.DirectoryID != 0 && video.DirectoryID != targetDir.ID {
		if oldDir, err := s.store.GetDirectory(r.Context(), video.DirectoryID); err == nil {
			s.startSyncDir(oldDir)
		}
	}
	s.serveVideoList(w, r)
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
	video, err := s.store.GetNextUnwatched(r.Context(), tagID)
	if err != nil {
		http.Error(w, "no unwatched videos", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": video.ID, "title": video.Title()}) //nolint:errcheck
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

func (s *server) handleQuickLabelModal(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	render(w, "quick_label_modal.html", video)
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
