// handlers_directories.go – directory management, filesystem browser,
// file import (drag-drop / upload), and yt-dlp download handlers.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

// ── Import (upload / drag-drop) ───────────────────────────────────────────────

// importFile saves the uploaded file to dir, registers it in the store, and
// tags it with the directory name. It is the shared business logic extracted
// from handleImportUpload.
func (s *server) importFile(ctx context.Context, dir store.Directory, fh io.Reader, origName string) (string, error) {
	ext := filepath.Ext(origName)
	stem := strings.TrimSuffix(origName, ext)

	out, savedName, err := openFreeFile(dir.Path, stem, ext)
	if err != nil {
		return "", fmt.Errorf("cannot create file: %w", err)
	}
	if _, err := io.Copy(out, fh); err != nil {
		out.Close()
		os.Remove(filepath.Join(dir.Path, savedName)) //nolint:errcheck
		return "", fmt.Errorf("write failed: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(filepath.Join(dir.Path, savedName)) //nolint:errcheck
		return "", fmt.Errorf("flush failed: %w", err)
	}
	v, err := s.store.UpsertVideo(ctx, dir.ID, dir.Path, savedName)
	if err != nil {
		slog.Warn("import: upsert video failed", "dir", dir.Path, "filename", savedName, "err", err)
	} else {
		dirTag, err := s.store.UpsertTag(ctx, filepath.Base(dir.Path))
		if err == nil {
			_ = s.store.TagVideo(ctx, v.ID, dirTag.ID)
		}
	}
	return savedName, nil
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

	// Cap the body to prevent a malicious client from exhausting memory/disk.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(multipartMemBytes); err != nil {
		http.Error(w, "cannot parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	fh, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file in upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer fh.Close()

	// Strip directory components from the client-supplied filename to
	// prevent path traversal (e.g. "../../etc/cron.d/x").
	origName := filepath.Base(strings.TrimSpace(r.FormValue("filename")))
	if origName == "" || origName == "." || !isVideoFile(origName) {
		http.Error(w, "not a supported video file", http.StatusBadRequest)
		return
	}

	if _, err := s.importFile(r.Context(), dir, fh, origName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Directories ───────────────────────────────────────────────────────────────

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
// and syncs it.  Creation is restricted to the user's home-directory subtree
// to prevent an authenticated client from creating directories anywhere on the
// filesystem (e.g. /etc/cron.d/).
func (s *server) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
		return
	}
	cleaned := filepath.Clean(path)
	rel, relErr := filepath.Rel(home, cleaned)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "path must be inside your home directory", http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(cleaned, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.addAndSyncDir(w, r, cleaned)
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

// handleCreateSubfolder creates a new directory named <name> inside the
// registered directory identified by {id}, then registers and syncs it.
func (s *server) handleCreateSubfolder(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	parent, err := s.store.GetDirectory(r.Context(), id)
	if err != nil {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "folder name required", http.StatusBadRequest)
		return
	}
	// Reject names that contain path separators or parent-directory references
	// to prevent path traversal (e.g. "../evil", "a/b").
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		http.Error(w, "folder name must not contain path separators or '..'", http.StatusBadRequest)
		return
	}
	path := filepath.Join(parent.Path, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		http.Error(w, "could not create folder: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.addAndSyncDir(w, r, path)
}

// handleBrowseFS lists the immediate visible subdirectories of a path.
// It is used by the folder-picker UI in the library sidebar.
// The path defaults to the user's home directory when not supplied.
// Browsing is restricted to the home-directory subtree to limit filesystem
// exposure.  Symlinks are resolved via filepath.EvalSymlinks before the
// boundary check so that a symlink inside home pointing outside cannot be
// used to escape the restriction.
func (s *server) handleBrowseFS(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	// Resolve symlinks in home itself (rare but possible on macOS with /private).
	if realHome, err2 := filepath.EvalSymlinks(home); err2 == nil {
		home = realHome
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = home
	}
	path = filepath.Clean(path)

	// Resolve symlinks so a symlink inside home pointing outside cannot be
	// used to read arbitrary directories.
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		http.Error(w, "cannot resolve path: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reject paths outside the home directory.
	rel, err := filepath.Rel(home, realPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "path is outside the allowed directory", http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(realPath)
	if err != nil {
		http.Error(w, "cannot read directory: "+err.Error(), http.StatusBadRequest)
		return
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, filepath.Join(realPath, e.Name()))
	}

	parent := filepath.Dir(realPath)
	if parent == realPath || parent == home { // already at root or at home boundary
		parent = ""
	}

	data := struct {
		Path    string
		Parent  string
		Entries []string
	}{realPath, parent, dirs}

	render(w, "dir_browser.html", data)
}

// ── yt-dlp download ───────────────────────────────────────────────────────────

func (s *server) handleYTDLPDownload(w http.ResponseWriter, r *http.Request) {
	// Validate all inputs before checking binary availability so that bad
	// requests get proper 4xx responses even when yt-dlp is not installed.
	rawURLs := strings.TrimSpace(r.FormValue("urls"))
	if rawURLs == "" {
		http.Error(w, "urls required", http.StatusBadRequest)
		return
	}
	// Split on newlines; validate each URL allows only http/https to prevent
	// SSRF via file://, ftp://, or internal network schemes that yt-dlp accepts.
	var urls []string
	for _, line := range strings.Split(rawURLs, "\n") {
		u := strings.TrimSpace(line)
		if u == "" {
			continue
		}
		parsed, parseErr := url.Parse(u)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			http.Error(w, "only http:// and https:// URLs are permitted", http.StatusBadRequest)
			return
		}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		http.Error(w, "no valid URLs provided", http.StatusBadRequest)
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

	if _, err := exec.LookPath("yt-dlp"); err != nil {
		http.Error(w, "yt-dlp is not installed — downloading is unavailable", http.StatusServiceUnavailable)
		return
	}

	// Create a job for each URL; launch each in its own goroutine.
	type jobEntry struct {
		JobID string
		URL   string
	}
	var entries []jobEntry
	for _, rawURL := range urls {
		jobID := newToken()

		// 4096 lines: yt-dlp output is typically low-volume, but playlists
		// or verbose modes can produce many lines.  The non-blocking send
		// drops lines when the buffer fills rather than blocking the goroutine.
		job := &ytdlpJob{ch: make(chan string, 4096)}
		s.jobsMu.Lock()
		s.jobs[jobID] = job
		s.jobsMu.Unlock()
		entries = append(entries, jobEntry{JobID: jobID, URL: rawURL})

		// Run the download in the background so the POST returns quickly.
		go func() {
			defer scheduleJobCleanup(job.ch, func() {
				s.jobsMu.Lock()
				delete(s.jobs, jobID)
				s.jobsMu.Unlock()
			})

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
				"--write-info-json",
				"--no-write-thumbnail",
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
			// Also watch for the "Destination:" line to capture the output path.
			var videoPath string
			scanDone := make(chan struct{})
			go func() {
				defer close(scanDone)
				sc := bufio.NewScanner(pr)
				for sc.Scan() {
					line := sc.Text()
					send(line)
					// Capture destination path from yt-dlp output.
					// Prefer [Merger] line (merged 1080p+ streams) over [download] Destination.
					if p, ok2 := strings.CutPrefix(line, "[Merger] Merging formats into \""); ok2 {
						videoPath = strings.TrimSuffix(strings.TrimSpace(p), "\"")
					} else if p, ok2 := strings.CutPrefix(line, "[download] Destination: "); ok2 {
						if videoPath == "" {
							videoPath = strings.TrimSpace(p)
						}
					} else if after, ok3 := strings.CutPrefix(line, "[download] "); ok3 {
						if idx := strings.Index(after, " has already been downloaded"); idx > 0 {
							if videoPath == "" {
								videoPath = strings.TrimSpace(after[:idx])
							}
						}
					}
				}
			}()
			job.err = cmd.Wait()
			pw.Close()
			<-scanDone

			if job.err == nil {
				// Tag the video file with metadata from the info.json.
				if videoPath != "" {
					infoJSON := videoPath + ".info.json"
					if data, err := os.ReadFile(infoJSON); err == nil {
						if u, ok2 := parseYTDLPInfoJSON(data); ok2 {
							send("[video_manger] Writing metadata to file…")
							if err := metadata.Write(videoPath, u); err != nil {
								send("[video_manger] Warning: metadata write failed: " + err.Error())
							}
						}
						os.Remove(infoJSON) //nolint:errcheck
					}
				}
				send("[video_manger] Syncing library…")
				s.syncDir(dir)
				if videoPath != "" {
					if v, verr := s.store.UpsertVideo(context.Background(), dir.ID, dir.Path, filepath.Base(videoPath)); verr == nil {
						job.videoID = v.ID
					}
				}
				send("[video_manger] Done!")
			}
		}()
	}

	// Return one progress block per queued URL.
	render(w, "ytdlp_progress.html", entries)
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
		sse.Event("downloadError", job.err.Error())
	} else {
		if job.videoID > 0 {
			sse.Event("videoReady", strconv.FormatInt(job.videoID, 10))
		}
		sse.Event("done", "")
	}
}

// parseYTDLPInfoJSON converts a yt-dlp .info.json file into a metadata.Updates
// that can be written directly to the video file via ffmpeg stream-copy.
func parseYTDLPInfoJSON(data []byte) (metadata.Updates, bool) {
	var info struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Uploader    string   `json:"uploader"`
		Channel     string   `json:"channel"`
		UploadDate  string   `json:"upload_date"`  // YYYYMMDD
		ReleaseDate string   `json:"release_date"` // YYYYMMDD or empty
		Tags        []string `json:"tags"`
		Categories  []string `json:"categories"`
		Genre       string   `json:"genre"`
		Series      string   `json:"series"`
		SeasonNum   int      `json:"season_number"`
		EpisodeNum  int      `json:"episode_number"`
		EpisodeID   string   `json:"episode_id"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return metadata.Updates{}, false
	}

	formatDate := func(d string) string {
		if len(d) == 8 {
			return d[:4] + "-" + d[4:6] + "-" + d[6:]
		}
		return d
	}
	date := formatDate(info.ReleaseDate)
	if date == "" {
		date = formatDate(info.UploadDate)
	}

	network := info.Channel
	if network == "" {
		network = info.Uploader
	}

	genre := info.Genre
	if genre == "" && len(info.Categories) > 0 {
		genre = info.Categories[0]
	}

	keywords := info.Tags

	u := metadata.Updates{
		Title:       strPtr(info.Title),
		Description: strPtr(info.Description),
		Genre:       strPtr(genre),
		Date:        strPtr(date),
		Keywords:    keywords,
		Network:     strPtr(network),
	}
	if info.Series != "" {
		u.Show = strPtr(info.Series)
	}
	if info.SeasonNum > 0 {
		s := fmt.Sprintf("%d", info.SeasonNum)
		u.SeasonNum = &s
	}
	if info.EpisodeNum > 0 {
		e := fmt.Sprintf("%d", info.EpisodeNum)
		u.EpisodeNum = &e
	}
	if info.EpisodeID != "" {
		u.EpisodeID = strPtr(info.EpisodeID)
	}
	return u, true
}

