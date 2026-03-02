package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
	"github.com/maxgarvey/video_manger/transcode"
)

// localAddresses returns http:// URLs for each non-loopback IPv4 address
// on the machine, using the given port.
func localAddresses(port string) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			result = append(result, "http://"+ip.String()+":"+port)
		}
	}
	return result
}

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
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
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

	libPath, _ := s.store.GetSetting(r.Context(), "library_path")
	data := struct {
		Video        store.Video
		Tags         []store.Tag
		AllTags      []store.Tag
		FileNotFound bool
		LibraryPath  string
	}{video, tags, allTags, fileNotFound, strings.TrimSpace(libPath)}
	render(w, "player.html", data)
}

func (s *server) handleVideoFile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, video.FilePath())
}

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
	w.Write([]byte(video.Title())) //nolint
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
	render(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags})
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
	tag, err := s.store.UpsertTag(r.Context(), tagName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.TagVideo(r.Context(), id, tag.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err == nil {
		s.syncTagsToFile(r.Context(), video)
	}
	render(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags})
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
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err == nil {
		s.syncTagsToFile(r.Context(), video)
	}
	render(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags})
}

func (s *server) handleVideoDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	render(w, "video_delete_confirm.html", video)
}

func (s *server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteVideo(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.PruneOrphanTags(r.Context()); err != nil {
		slog.Warn("prune orphan tags failed", "err", err)
	}
	s.serveVideoList(w, r)
}

func (s *server) handleDeleteVideoAndFile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := s.store.DeleteVideo(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Remove(video.FilePath()); err != nil {
		slog.Warn("delete file failed", "path", video.FilePath(), "err", err)
	}
	s.serveVideoList(w, r)
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

	// Find or create a directory record for the parent dir.
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var dirID int64
	for _, d := range dirs {
		if d.Path == newDir {
			dirID = d.ID
			break
		}
	}
	if dirID == 0 {
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
	switch {
	case isSearch:
		videos, err = s.store.SearchVideos(r.Context(), q.Get("q"))
	case q.Get("tag_id") != "":
		tagID, _ := strconv.ParseInt(q.Get("tag_id"), 10, 64)
		videos, err = s.store.ListVideosByTag(r.Context(), tagID)
	case q.Get("rating") != "":
		minRating, _ := strconv.Atoi(q.Get("rating"))
		if minRating < 1 {
			minRating = 1
		}
		videos, err = s.store.ListVideosByMinRating(r.Context(), minRating)
	case sortOrder == "rating":
		videos, err = s.store.ListVideosByRating(r.Context())
	default:
		videos, err = s.store.ListVideos(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Apply rating sort uniformly regardless of which filter was used.
	if sortOrder == "rating" {
		slices.SortFunc(videos, func(a, b store.Video) int {
			if a.Rating != b.Rating {
				return b.Rating - a.Rating // higher rating first
			}
			if a.Title() < b.Title() {
				return -1
			}
			if a.Title() > b.Title() {
				return 1
			}
			return 0
		})
	} else if !isSearch {
		// For non-search views, sort by directory then title so groups are contiguous.
		slices.SortFunc(videos, func(a, b store.Video) int {
			if a.DirectoryPath != b.DirectoryPath {
				if a.DirectoryPath < b.DirectoryPath {
					return -1
				}
				return 1
			}
			if a.Title() < b.Title() {
				return -1
			}
			if a.Title() > b.Title() {
				return 1
			}
			return 0
		})
	}
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

	history, _ := s.store.ListWatchHistory(r.Context())
	data := struct {
		Groups   []videoGroup
		History  map[int64]store.WatchRecord
		Page     int
		PageSize int
		Total    int
	}{groupVideosByDir(pageVideos), history, page, limit, total}
	render(w, "video_list.html", data)
}

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

func (s *server) handleCopyToLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	libPath, _ := s.store.GetSetting(r.Context(), "library_path")
	libPath = strings.TrimSpace(libPath)
	if libPath == "" {
		http.Error(w, "Library path not configured — set it in Settings.", http.StatusBadRequest)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

// copyFile copies src to dst using a streaming io.Copy.
// If the write fails, the partial destination file is removed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) //nolint:errcheck
		return err
	}
	return out.Close()
}

func (s *server) handleImportUpload(w http.ResponseWriter, r *http.Request) {
	dirID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("dir_id")), 10, 64)
	if err != nil {
		http.Error(w, "invalid dir_id", http.StatusBadRequest)
		return
	}
	dir, err := s.store.GetDirectory(r.Context(), dirID)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	// S4: cap the body so a malicious client cannot exhaust memory/disk.
	r.Body = http.MaxBytesReader(w, r.Body, 8<<30) // 8 GB
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "cannot parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	fh, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file in upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer fh.Close()

	// S1: strip directory components from the client-supplied filename to
	// prevent path traversal (e.g. "../../etc/cron.d/x").
	origName := filepath.Base(strings.TrimSpace(r.FormValue("filename")))
	if origName == "" || origName == "." || !isVideoFile(origName) {
		http.Error(w, "not a supported video file", http.StatusBadRequest)
		return
	}
	ext := filepath.Ext(origName)
	stem := strings.TrimSuffix(origName, ext)

	// R8: atomically create the destination file with O_EXCL so no two
	// concurrent uploads can race to the same filename.
	out, savedName, err := openFreeFile(dir.Path, stem, ext)
	if err != nil {
		http.Error(w, "cannot create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// R1: single explicit close path; clean up on write failure.
	if _, err := io.Copy(out, fh); err != nil {
		out.Close()
		os.Remove(filepath.Join(dir.Path, savedName)) //nolint:errcheck
		http.Error(w, "write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := out.Close(); err != nil {
		os.Remove(filepath.Join(dir.Path, savedName)) //nolint:errcheck
		http.Error(w, "flush failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	v, err := s.store.UpsertVideo(r.Context(), dir.ID, dir.Path, savedName)
	if err != nil {
		slog.Warn("import: upsert video failed", "dir", dir.Path, "filename", savedName, "err", err)
	} else {
		dirTag, err := s.store.UpsertTag(r.Context(), filepath.Base(dir.Path))
		if err == nil {
			_ = s.store.TagVideo(r.Context(), v.ID, dirTag.ID)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// openFreeFile atomically creates a new file in dir using O_CREATE|O_EXCL,
// appending a counter suffix (_2, _3, …) if the base name is already taken.
// Returns the open file handle and the final filename chosen.
func openFreeFile(dir, stem, ext string) (*os.File, string, error) {
	try := func(name string) (*os.File, error) {
		return os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	}
	name := stem + ext
	f, err := try(name)
	if err == nil {
		return f, name, nil
	}
	if !os.IsExist(err) {
		return nil, "", err
	}
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s_%d%s", stem, i, ext)
		f, err := try(name)
		if err == nil {
			return f, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
}

func (s *server) handleYTDLPDownload(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.FormValue("url"))
	if rawURL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	dirIDStr := strings.TrimSpace(r.FormValue("dir_id"))
	if dirIDStr == "" {
		http.Error(w, "dir_id required", http.StatusBadRequest)
		return
	}
	dirID, err := strconv.ParseInt(dirIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid dir_id", http.StatusBadRequest)
		return
	}
	dir, err := s.store.GetDirectory(r.Context(), dirID)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	// Generate an opaque job ID.
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "could not generate job id", http.StatusInternalServerError)
		return
	}
	jobID := hex.EncodeToString(raw)

	job := &ytdlpJob{ch: make(chan string, 2048)}
	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()

	// Run the download in the background so the POST returns quickly.
	go func() {
		defer func() {
			close(job.ch)
			// Retain job for 10 minutes so late SSE clients can still read it.
			time.AfterFunc(10*time.Minute, func() {
				s.jobsMu.Lock()
				delete(s.jobs, jobID)
				s.jobsMu.Unlock()
			})
		}()

		// Non-blocking send to avoid goroutine leaks if the channel fills.
		send := func(line string) {
			select {
			case job.ch <- line:
			default:
			}
		}

		// Wait for a concurrency slot (same limit as ffmpeg operations).
		send("[queue] Waiting for download slot…")
		s.convertSem <- struct{}{}
		defer func() { <-s.convertSem }()

		pr, pw := io.Pipe()
		cmd := exec.Command("yt-dlp", //nolint:gosec
			"--no-playlist",
			"--newline",
			"-o", filepath.Join(dir.Path, "%(title)s.%(ext)s"),
			rawURL,
		)
		cmd.Stdout = pw
		cmd.Stderr = pw

		if err := cmd.Start(); err != nil {
			job.err = err
			return
		}

		// Forward yt-dlp output lines to the job channel while it runs.
		scanDone := make(chan struct{})
		go func() {
			defer close(scanDone)
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				send(sc.Text())
			}
		}()
		job.err = cmd.Wait()
		pw.Close()
		<-scanDone

		if job.err == nil {
			send("[video_manger] Syncing library…")
			s.syncDir(dir)
			send("[video_manger] Done!")
		}
	}()

	// Return a progress container that streams output via SSE.
	render(w, "ytdlp_progress.html", jobID)
}

// handleYTDLPJobEvents streams yt-dlp output for a background download job
// as Server-Sent Events. Sends a "done" or "error" event when the job finishes.
func (s *server) handleYTDLPJobEvents(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	s.jobsMu.Lock()
	job, ok := s.jobs[jobID]
	s.jobsMu.Unlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for line := range job.ch {
		// SSE data fields cannot contain bare newlines.
		safe := strings.ReplaceAll(strings.ReplaceAll(line, "\r", ""), "\n", " ")
		fmt.Fprintf(w, "data: %s\n\n", safe)
		flusher.Flush()
	}

	if job.err != nil {
		msg := strings.ReplaceAll(job.err.Error(), "\n", " ")
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", msg)
	} else {
		fmt.Fprintf(w, "event: done\ndata: \n\n")
	}
	flusher.Flush()
}

func (s *server) handleConvert(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.FormValue("format")))
	cf, ok := transcode.Formats[format]
	if !ok {
		http.Error(w, "format must be mp4, webm, or mkv", http.StatusBadRequest)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	ext := filepath.Ext(video.Filename)
	base := strings.TrimSuffix(video.Filename, ext)

	// Guard against overwriting the source file (e.g. mkv→mkv with copy codec).
	if strings.EqualFold(ext, cf.Ext) {
		http.Error(w, "source and output are the same file; choose a different format", http.StatusBadRequest)
		return
	}

	outName := freeOutputName(video.DirectoryPath, base, "", cf.Ext)
	outPath := filepath.Join(video.DirectoryPath, outName)

	// Use a background context so the conversion is not killed if the browser
	// disconnects mid-way. The file will be picked up by the next library poll.
	bgCtx := context.WithoutCancel(r.Context())
	if err := transcode.Convert(bgCtx, s.convertSem, video.FilePath(), outPath, cf); err != nil {
		os.Remove(outPath) //nolint:errcheck
		slog.Error("ffmpeg convert failed", "src", video.FilePath(), "dst", outPath, "err", err)
		http.Error(w, "conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Register the converted file in the library.
	if video.DirectoryID != 0 {
		if dir, err := s.store.GetDirectory(bgCtx, video.DirectoryID); err == nil {
			if _, err := s.store.UpsertVideo(bgCtx, dir.ID, dir.Path, outName); err != nil {
				slog.Warn("register converted file failed", "filename", outName, "err", err)
			}
		}
	}

	s.serveVideoList(w, r)
}

func (s *server) handleExportUSB(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	// Build output path: same directory, with _usb suffix.
	ext := filepath.Ext(video.Filename)
	base := strings.TrimSuffix(video.Filename, ext)
	outName := base + "_usb.mp4"
	outPath := filepath.Join(video.DirectoryPath, outName)

	bgCtx := context.WithoutCancel(r.Context())
	if err := transcode.ExportUSB(bgCtx, s.convertSem, video.FilePath(), outPath); err != nil {
		slog.Error("ffmpeg export failed", "path", video.FilePath(), "err", err)
		http.Error(w, "export failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Clean up the transcoded file after serving so it does not accumulate
	// in the source directory or appear in a subsequent library sync.
	defer os.Remove(outPath) //nolint:errcheck
	w.Header().Set("Content-Disposition", `attachment; filename="`+outName+`"`)
	http.ServeFile(w, r, outPath)
}

func (s *server) handleSetRating(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	// Verify the video exists before updating (SetVideoRating is a blind UPDATE).
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}
	rating, _ := strconv.Atoi(r.FormValue("rating"))
	if rating < 0 || rating > 2 {
		http.Error(w, "rating must be 0, 1, or 2", http.StatusBadRequest)
		return
	}
	if err := s.store.SetVideoRating(r.Context(), id, rating); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err = s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "rating_buttons.html", video)
}

func (s *server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("ffprobe failed", "path", video.FilePath(), "err", err)
	}
	streams, err := metadata.ReadStreams(video.FilePath())
	if err != nil {
		slog.Warn("ffprobe streams failed", "path", video.FilePath(), "err", err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
		Streams []metadata.Stream
	}{id, native, streams}
	render(w, "file_metadata.html", data)
}

func (s *server) handleEditMetadata(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("ffprobe failed", "path", video.FilePath(), "err", err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	render(w, "file_metadata_edit.html", data)
}

func (s *server) handleUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	strPtr := func(key string) *string {
		v := r.FormValue(key)
		return &v
	}
	u := metadata.Updates{
		Title:       strPtr("title"),
		Description: strPtr("description"),
		Genre:       strPtr("genre"),
		Date:        strPtr("date"),
		Comment:     strPtr("comment"),
		Show:        strPtr("show"),
		Network:     strPtr("network"),
		EpisodeID:   strPtr("episode_id"),
		SeasonNum:   strPtr("season_number"),
		EpisodeNum:  strPtr("episode_sort"),
	}
	if err := metadata.Write(video.FilePath(), u); err != nil {
		slog.Warn("metadata write failed", "path", video.FilePath(), "err", err)
		// Degrade gracefully: show the unchanged read view rather than a 500.
	}
	// Return the updated read-only view
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("ffprobe failed", "path", video.FilePath(), "err", err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	render(w, "file_metadata.html", data)
}

func (s *server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "tags.html", tags)
}

func (s *server) handleDirectoryDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	dir, err := s.store.GetDirectory(r.Context(), id)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	render(w, "directory_delete_confirm.html", dir)
}

func (s *server) handleDeleteDirectoryAndFiles(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	// Atomically delete all video records and the directory in a single
	// transaction, then remove the files from disk on a best-effort basis.
	paths, err := s.store.DeleteDirectoryAndVideos(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			slog.Warn("delete file failed", "path", p, "err", err)
		}
	}
	s.serveDirList(w, r)
}

// --- Metadata lookup (TMDB) ---

type tmdbSearchResult struct {
	ID          int     `json:"id"`
	MediaType   string  `json:"media_type"`
	Title       string  `json:"title"` // movies
	Name        string  `json:"name"`  // TV
	Overview    string  `json:"overview"`
	ReleaseDate string  `json:"release_date"`
	FirstAir    string  `json:"first_air_date"`
	Popularity  float64 `json:"popularity"`
}

func (r tmdbSearchResult) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r tmdbSearchResult) Year() string {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAir
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

type tmdbMovieDetail struct {
	Title    string                  `json:"title"`
	Overview string                  `json:"overview"`
	Genres   []struct{ Name string } `json:"genres"`
	Release  string                  `json:"release_date"`
}

type tmdbEpisodeDetail struct {
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	AirDate    string `json:"air_date"`
	EpisodeNum int    `json:"episode_number"`
	SeasonNum  int    `json:"season_number"`
	ShowName   string // populated from series call
}

// tmdbClient is a dedicated HTTP client for TMDB API calls with a
// conservative timeout so a slow or unresponsive TMDB doesn't hang handlers.
var tmdbClient = &http.Client{Timeout: 15 * time.Second}

func tmdbGet(apiKey, path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, "https://api.themoviedb.org"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("TMDB %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (s *server) handleLookupModal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	apiKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	data := struct {
		VideoID int64
		HasKey  bool
	}{id, apiKey != ""}
	render(w, "lookup_modal.html", data)
}

func (s *server) handleLookupSearch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	apiKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	if apiKey == "" {
		http.Error(w, "TMDB API key not configured", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(r.FormValue("q"))
	if q == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}

	path := "/3/search/multi?query=" + url.QueryEscape(q) + "&include_adult=false"
	var result struct {
		Results []tmdbSearchResult `json:"results"`
	}
	if err := tmdbGet(apiKey, path, &result); err != nil {
		slog.Warn("TMDB search failed", "query", q, "err", err)
		http.Error(w, "TMDB search failed", http.StatusBadGateway)
		return
	}

	// Limit to top 10.
	if len(result.Results) > 10 {
		result.Results = result.Results[:10]
	}

	data := struct {
		VideoID int64
		Results []tmdbSearchResult
	}{id, result.Results}
	render(w, "lookup_results.html", data)
}

func (s *server) handleLookupApply(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	apiKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	if apiKey == "" {
		http.Error(w, "TMDB API key not configured", http.StatusBadRequest)
		return
	}

	mediaType := r.FormValue("media_type")
	tmdbID := r.FormValue("tmdb_id")

	var u metadata.Updates
	switch mediaType {
	case "movie":
		var detail tmdbMovieDetail
		if err := tmdbGet(apiKey, "/3/movie/"+tmdbID, &detail); err != nil {
			slog.Warn("TMDB movie fetch failed", "tmdbID", tmdbID, "err", err)
			http.Error(w, "TMDB movie lookup failed", http.StatusBadGateway)
			return
		}
		genre := ""
		if len(detail.Genres) > 0 {
			genre = detail.Genres[0].Name
		}
		u = metadata.Updates{
			Title:       &detail.Title,
			Description: &detail.Overview,
			Genre:       &genre,
			Date:        &detail.Release,
		}
	case "tv":
		seasonStr := r.FormValue("season")
		episodeStr := r.FormValue("episode")
		// Fetch series name — best-effort; log but do not abort.
		var series struct {
			Name string `json:"name"`
		}
		if err := tmdbGet(apiKey, "/3/tv/"+tmdbID, &series); err != nil {
			slog.Warn("TMDB series fetch failed", "tmdbID", tmdbID, "err", err)
		}
		var ep tmdbEpisodeDetail
		epPath := fmt.Sprintf("/3/tv/%s/season/%s/episode/%s", tmdbID, seasonStr, episodeStr)
		if err := tmdbGet(apiKey, epPath, &ep); err != nil {
			http.Error(w, "TMDB episode lookup failed", http.StatusBadGateway)
			return
		}
		epID := fmt.Sprintf("S%02dE%02d", ep.SeasonNum, ep.EpisodeNum)
		seasonNumStr := fmt.Sprintf("%d", ep.SeasonNum)
		episodeNumStr := fmt.Sprintf("%d", ep.EpisodeNum)
		u = metadata.Updates{
			Title:       &ep.Name,
			Description: &ep.Overview,
			Show:        &series.Name,
			EpisodeID:   &epID,
			SeasonNum:   &seasonNumStr,
			EpisodeNum:  &episodeNumStr,
			Date:        &ep.AirDate,
		}
	default:
		http.Error(w, "media_type must be movie or tv", http.StatusBadRequest)
		return
	}

	if err := metadata.Write(video.FilePath(), u); err != nil {
		slog.Error("lookup apply metadata write failed", "path", video.FilePath(), "err", err)
		http.Error(w, "metadata write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync the TMDB title into the DB display_name so the library sidebar
	// reflects the new title without requiring a manual re-sync.
	if u.Title != nil && *u.Title != "" {
		if err := s.store.UpdateVideoName(r.Context(), id, *u.Title); err != nil {
			slog.Warn("update display_name after TMDB apply failed", "videoID", id, "err", err)
		}
	}

	// Refresh the metadata view.
	native, _ := metadata.Read(video.FilePath())
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	render(w, "file_metadata.html", data)
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	autoplay, _ := s.store.GetSetting(r.Context(), "autoplay_random")
	sortOrder, _ := s.store.GetSetting(r.Context(), "video_sort")
	tmdbKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	libPath, _ := s.store.GetSetting(r.Context(), "library_path")
	data := struct {
		AutoplayRandom bool
		VideoSort      string
		HasTMDBKey     bool
		LibraryPath    string
	}{
		AutoplayRandom: autoplay != "false",
		VideoSort:      sortOrder,
		HasTMDBKey:     tmdbKey != "",
		LibraryPath:    libPath,
	}
	render(w, "settings.html", data)
}

func (s *server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	autoplay := "false"
	if r.FormValue("autoplay_random") == "on" {
		autoplay = "true"
	}
	sortOrder := r.FormValue("video_sort")
	if sortOrder != "name" && sortOrder != "rating" {
		sortOrder = "name"
	}
	settings := map[string]string{
		"autoplay_random": autoplay,
		"video_sort":      sortOrder,
		"library_path":    strings.TrimSpace(r.FormValue("library_path")),
	}
	// Only overwrite the key if the user submitted a non-empty value; leaving
	// the field blank preserves the existing key.
	if newKey := strings.TrimSpace(r.FormValue("tmdb_api_key")); newKey != "" {
		settings["tmdb_api_key"] = newKey
	}
	if err := s.store.SaveSettings(r.Context(), settings); err != nil {
		http.Error(w, "save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.handleGetSettings(w, r)
}

func (s *server) serveDirList(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.syncingMu.Lock()
	syncing := make(map[int64]bool, len(s.syncingDirs))
	for id := range s.syncingDirs {
		syncing[id] = true
	}
	s.syncingMu.Unlock()
	data := struct {
		Dirs    []store.Directory
		Syncing map[int64]bool
	}{dirs, syncing}
	render(w, "directories.html", data)
}

func (s *server) handleSyncDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	dir, err := s.store.GetDirectory(r.Context(), id)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	s.startSyncDir(dir)
	s.serveDirList(w, r)
}

func (s *server) handleDirectoryOptions(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "directory_options.html", dirs)
}

// addAndSyncDir registers path in the DB, starts an async sync, then renders
// the updated directory list (which shows a spinner for the in-progress dir).
// It is the shared tail of handleAddDirectory and handleCreateDirectory.
func (s *server) addAndSyncDir(w http.ResponseWriter, r *http.Request, path string) {
	d, err := s.store.AddDirectory(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.startSyncDir(d)
	s.serveDirList(w, r)
}

// handleCreateDirectory creates the directory on disk (MkdirAll) then registers
// and syncs it.
func (s *server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.addAndSyncDir(w, r, path)
}

func (s *server) handleAddDirectory(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	s.addAndSyncDir(w, r, path)
}

func (s *server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteDirectory(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.serveDirList(w, r)
}

// handleBrowseFS lists the immediate visible subdirectories of a path.
// It is used by the folder-picker UI in the library sidebar.
// The path defaults to the user's home directory when not supplied.
// Browsing is restricted to the home-directory subtree to limit filesystem exposure.
func (s *server) handleBrowseFS(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = home
	}
	path = filepath.Clean(path)

	// Reject paths outside the home directory.
	rel, err := filepath.Rel(home, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "path is outside the allowed directory", http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(w, "cannot read directory: "+err.Error(), http.StatusBadRequest)
		return
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, filepath.Join(path, e.Name()))
	}

	parent := filepath.Dir(path)
	if parent == path || parent == home { // already at root or at home boundary
		parent = ""
	}

	data := struct {
		Path    string
		Parent  string
		Entries []string
	}{path, parent, dirs}

	render(w, "dir_browser.html", data)
}

// --- P2P sharing ---

func (s *server) handleSharePanel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetVideo(r.Context(), id); err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}
	suffix := fmt.Sprintf("/video/%d", id)
	addrs := localAddresses(s.port)
	links := make([]string, 0, len(addrs)+1)
	if s.mdnsName != "" {
		links = append(links, "http://"+s.mdnsName+":"+s.port+suffix)
	}
	for _, a := range addrs {
		links = append(links, a+suffix)
	}
	data := struct {
		VideoID int64
		Links   []string
	}{id, links}
	render(w, "share_panel.html", data)
}

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

func (s *server) handleTrimPanel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	render(w, "trim_panel.html", id)
}

func (s *server) handleTrim(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	start := strings.TrimSpace(r.FormValue("start"))
	end := strings.TrimSpace(r.FormValue("end"))
	if start == "" {
		start = "0"
	}

	ext := filepath.Ext(video.Filename)
	base := strings.TrimSuffix(video.Filename, ext)
	outName := freeOutputName(video.DirectoryPath, base, "_trim", ext)
	outPath := filepath.Join(video.DirectoryPath, outName)

	bgCtx := context.WithoutCancel(r.Context())
	if err := transcode.Trim(bgCtx, s.convertSem, video.FilePath(), outPath, start, end); err != nil {
		os.Remove(outPath) //nolint:errcheck
		slog.Error("ffmpeg trim failed", "path", video.FilePath(), "err", err)
		http.Error(w, "trim failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if video.DirectoryID != 0 {
		if dir, err := s.store.GetDirectory(bgCtx, video.DirectoryID); err == nil {
			if _, err := s.store.UpsertVideo(bgCtx, dir.ID, dir.Path, outName); err != nil {
				slog.Warn("register trimmed file failed", "filename", outName, "err", err)
			}
		}
	}

	s.serveVideoList(w, r)
}

// freeOutputName returns the first non-existing filename of the form
// base+suffix+ext, base+suffix_2+ext, base+suffix_3+ext, … inside dir.
func freeOutputName(dir, base, suffix, ext string) string {
	candidate := base + suffix + ext
	if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; ; i++ {
		candidate = fmt.Sprintf("%s%s_%d%s", base, suffix, i, ext)
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}
