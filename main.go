package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/grandcat/zeroconf"
	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

//go:embed templates/*
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"base":    filepath.Base,
	"reltime": reltime,
}).ParseFS(templateFS, "templates/*.html"))

// reltime formats a SQLite datetime string (UTC, "2006-01-02 15:04:05") as a
// human-readable relative duration: "just now", "5 mins ago", "yesterday", "Jan 2".
func reltime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return s
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hr ago"
		}
		return fmt.Sprintf("%d hrs ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

type server struct {
	store    store.Store
	port     string
	mdnsName string // e.g. "video-manger.local"
}

func main() {
	dbPath := flag.String("db", "video_manger.db", "path to SQLite database file")
	dir := flag.String("dir", "", "video directory to register on startup (optional)")
	port := flag.String("port", "8080", "port to listen on")
	flag.Parse()

	s, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	srv := &server{store: s, port: *port, mdnsName: "video-manger.local"}

	if *dir != "" {
		d, err := srv.store.AddDirectory(context.Background(), *dir)
		if err != nil {
			log.Printf("warning: could not register dir %s: %v", *dir, err)
		} else {
			srv.syncDir(d)
		}
	}

	portInt, _ := strconv.Atoi(*port)
	mdns, err := zeroconf.Register("video-manger", "_http._tcp", "local.", portInt, nil, nil)
	if err != nil {
		log.Printf("mDNS register: %v (continuing without mDNS)", err)
	} else {
		defer mdns.Shutdown()
		log.Printf("  mDNS: http://video-manger.local:%s", *port)
	}

	go srv.startLibraryPoller(context.Background())

	log.Printf("Starting server on http://localhost:%s", *port)
	for _, addr := range localAddresses(*port) {
		log.Printf("  LAN: %s", addr)
	}
	log.Fatal(http.ListenAndServe(":"+*port, srv.routes()))
}

func (s *server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", s.handleIndex)
	r.Get("/info", s.handleInfo)

	// Videos
	r.Get("/videos", s.handleVideoList)
	r.Get("/play/random", s.handleRandomPlayer)
	r.Get("/play/{id}", s.handlePlayer)
	r.Get("/video/{id}", s.handleVideoFile)
	r.Put("/videos/{id}/name", s.handleUpdateVideoName)
	r.Get("/videos/{id}/delete-confirm", s.handleVideoDeleteConfirm)
	r.Delete("/videos/{id}", s.handleDeleteVideo)
	r.Delete("/videos/{id}/file", s.handleDeleteVideoAndFile)

	// Watch history
	r.Post("/videos/{id}/progress", s.handlePostProgress)
	r.Get("/videos/{id}/progress", s.handleGetProgress)

	// Rating
	r.Post("/videos/{id}/rating", s.handleSetRating)

	// Export / convert
	r.Post("/videos/{id}/export/usb", s.handleExportUSB)
	r.Post("/videos/{id}/convert", s.handleConvert)

	// yt-dlp download
	r.Post("/ytdlp/download", s.handleYTDLPDownload)

	// Metadata lookup (TMDB)
	r.Get("/videos/{id}/lookup", s.handleLookupModal)
	r.Post("/videos/{id}/lookup/search", s.handleLookupSearch)
	r.Post("/videos/{id}/lookup/apply", s.handleLookupApply)

	// P2P share
	r.Get("/videos/{id}/share", s.handleSharePanel)

	// File metadata (ffprobe/ffmpeg)
	r.Get("/videos/{id}/metadata", s.handleGetMetadata)
	r.Get("/videos/{id}/metadata/edit", s.handleEditMetadata)
	r.Put("/videos/{id}/metadata", s.handleUpdateMetadata)

	// Tags
	r.Get("/videos/{id}/tags", s.handleVideoTags)
	r.Post("/videos/{id}/tags", s.handleAddVideoTag)
	r.Delete("/videos/{id}/tags/{tagID}", s.handleRemoveVideoTag)
	r.Get("/tags", s.handleListTags)

	// Settings
	r.Get("/settings", s.handleGetSettings)
	r.Post("/settings", s.handleSaveSettings)

	// Filesystem browser (used by folder picker in sidebar)
	r.Get("/fs", s.handleBrowseFS)

	// Directories
	r.Get("/directories", s.handleListDirectories)
	r.Get("/directories/options", s.handleDirectoryOptions)
	r.Post("/directories", s.handleAddDirectory)
	r.Post("/directories/create", s.handleCreateDirectory)
	r.Get("/directories/{id}/delete-confirm", s.handleDirectoryDeleteConfirm)
	r.Delete("/directories/{id}", s.handleDeleteDirectory)
	r.Delete("/directories/{id}/files", s.handleDeleteDirectoryAndFiles)

	return r
}

// syncDir walks a directory tree recursively and upserts all video files into
// the store. Subdirectories are not registered as separate directory entries;
// all videos under the tree share the same directory_id but store their actual
// containing subdirectory path so FilePath() resolves correctly.
// If ffprobe is available, native title is read and used to pre-populate
// display_name for videos that don't yet have one set.
func (s *server) syncDir(d store.Directory) {
	if err := filepath.WalkDir(d.Path, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("sync walk %s: %v", path, err)
			return nil // keep walking
		}
		if de.IsDir() || !isVideoFile(de.Name()) {
			return nil
		}
		dir := filepath.Dir(path)
		v, err := s.store.UpsertVideo(context.Background(), d.ID, dir, de.Name())
		if err != nil {
			log.Printf("upsert %s: %v", path, err)
			return nil
		}
		if v.DisplayName == "" {
			if meta, err := metadata.Read(path); err == nil && meta.Title != "" {
				if err := s.store.UpdateVideoName(context.Background(), v.ID, meta.Title); err != nil {
					log.Printf("set native title %s: %v", path, err)
				}
			}
		}
		// Auto-tag with the registered directory's base name.
		dirTag, err := s.store.UpsertTag(context.Background(), filepath.Base(d.Path))
		if err != nil {
			log.Printf("upsert dir tag %s: %v", d.Path, err)
		} else if err := s.store.TagVideo(context.Background(), v.ID, dirTag.ID); err != nil {
			log.Printf("tag video %d with dir tag: %v", v.ID, err)
		}
		return nil
	}); err != nil {
		log.Printf("syncDir walk %s: %v", d.Path, err)
	}
}

// startLibraryPoller runs in the background, re-scanning all registered
// directories every 60 s so newly added files are picked up automatically.
func (s *server) startLibraryPoller(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dirs, err := s.store.ListDirectories(ctx)
			if err != nil {
				log.Printf("library poll: list dirs: %v", err)
				continue
			}
			for _, d := range dirs {
				s.syncDir(d)
			}
		}
	}
}

// syncTagsToFile writes the current DB tags for a video back to the file as keywords.
func (s *server) syncTagsToFile(ctx context.Context, video store.Video) {
	tags, err := s.store.ListTagsByVideo(ctx, video.ID)
	if err != nil {
		log.Printf("syncTagsToFile list tags %d: %v", video.ID, err)
		return
	}
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	if err := metadata.Write(video.FilePath(), metadata.Updates{Keywords: names}); err != nil {
		log.Printf("syncTagsToFile write %s: %v", video.FilePath(), err)
	}
}

// --- Handlers ---

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

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleVideoList(w http.ResponseWriter, r *http.Request) {
	s.serveVideoList(w, r)
}

func (s *server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	data := struct {
		Video   store.Video
		Tags    []store.Tag
		AllTags []store.Tag
	}{video, tags, allTags}
	if err := templates.ExecuteTemplate(w, "player.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleRandomPlayer(w http.ResponseWriter, r *http.Request) {
	autoplay, _ := s.store.GetSetting(r.Context(), "autoplay_random")
	if autoplay == "false" {
		w.Write([]byte(`<p style="color:#444">Select a video to play.</p>`)) //nolint:errcheck
		return
	}
	video, err := s.store.GetRandomVideo(r.Context())
	if err != nil {
		// No videos yet.
		w.Write([]byte(`<p style="color:#444">No videos yet — add a directory to get started.</p>`)) //nolint:errcheck
		return
	}
	tags, _ := s.store.ListTagsByVideo(r.Context(), video.ID)
	allTags, _ := s.store.ListTags(r.Context())
	data := struct {
		Video   store.Video
		Tags    []store.Tag
		AllTags []store.Tag
	}{video, tags, allTags}
	if err := templates.ExecuteTemplate(w, "player.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleVideoFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
			log.Printf("write title metadata %s: %v", video.FilePath(), err)
		}
	}
	w.Write([]byte(video.Title())) //nolint
}

func (s *server) handleVideoTags(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleAddVideoTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	if err := templates.ExecuteTemplate(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleRemoveVideoTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	s.store.PruneOrphanTags(r.Context()) //nolint:errcheck
	tags, err := s.store.ListTagsByVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err == nil {
		s.syncTagsToFile(r.Context(), video)
	}
	if err := templates.ExecuteTemplate(w, "video_tags.html", struct {
		VideoID int64
		Tags    []store.Tag
	}{id, tags}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleVideoDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := templates.ExecuteTemplate(w, "video_delete_confirm.html", video); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteVideo(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.store.PruneOrphanTags(r.Context()) //nolint:errcheck
	s.serveVideoList(w, r)
}

func (s *server) handleDeleteVideoAndFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
		log.Printf("delete file %s: %v", video.FilePath(), err)
	}
	s.serveVideoList(w, r)
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
	switch {
	case q.Get("q") != "":
		videos, err = s.store.SearchVideos(r.Context(), q.Get("q"))
	case q.Get("tag_id") != "":
		tagID, _ := strconv.ParseInt(q.Get("tag_id"), 10, 64)
		videos, err = s.store.ListVideosByTag(r.Context(), tagID)
	case sortOrder == "rating":
		videos, err = s.store.ListVideosByRating(r.Context())
	default:
		videos, err = s.store.ListVideos(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	watched, _ := s.store.ListWatchedIDs(r.Context())
	history, _ := s.store.ListWatchHistory(r.Context())
	data := struct {
		Videos  []store.Video
		Watched map[int64]bool
		History map[int64]store.WatchRecord
	}{videos, watched, history}
	if err := templates.ExecuteTemplate(w, "video_list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handlePostProgress(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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

	// Allow up to 10 minutes for large downloads.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--no-playlist",
		"-o", filepath.Join(dir.Path, "%(title)s.%(ext)s"),
		rawURL,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("yt-dlp %s: %v\nstderr: %s", rawURL, err, stderr.String())
		http.Error(w, "download failed: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	// Sync the directory to register the new file.
	s.syncDir(dir)
	s.serveVideoList(w, r)
}

type convertFormat struct {
	ext       string
	videoArgs []string
	audioArgs []string
}

var convertFormats = map[string]convertFormat{
	"mp4":  {".mp4", []string{"-c:v", "libx264"}, []string{"-c:a", "aac"}},
	"webm": {".webm", []string{"-c:v", "libvpx-vp9"}, []string{"-c:a", "libopus"}},
	"mkv":  {".mkv", []string{"-c:v", "copy"}, []string{"-c:a", "copy"}},
}

func (s *server) handleConvert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.FormValue("format")))
	cf, ok := convertFormats[format]
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
	outName := base + cf.ext
	outPath := filepath.Join(video.DirectoryPath, outName)

	// Guard against overwriting the source file (e.g. mkv→mkv with copy codec).
	if outPath == video.FilePath() {
		http.Error(w, "source and output are the same file; choose a different format", http.StatusBadRequest)
		return
	}

	args := []string{"-y", "-i", video.FilePath()}
	args = append(args, cf.videoArgs...)
	args = append(args, cf.audioArgs...)
	args = append(args, outPath)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(r.Context(), "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Remove any partial output file left behind by ffmpeg.
		os.Remove(outPath) //nolint:errcheck
		log.Printf("ffmpeg convert %s→%s: %v\n%s", video.FilePath(), outPath, err, stderr.String())
		http.Error(w, "conversion failed: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	// Register the converted file in the library.
	if video.DirectoryID != 0 {
		if dir, err := s.store.GetDirectory(r.Context(), video.DirectoryID); err == nil {
			if _, err := s.store.UpsertVideo(r.Context(), dir.ID, dir.Path, outName); err != nil {
				log.Printf("register converted file %s: %v", outName, err)
			}
		}
	}

	s.serveVideoList(w, r)
}

func (s *server) handleExportUSB(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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

	var stderr bytes.Buffer
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-y",
		"-i", video.FilePath(),
		"-c:v", "libx264", "-profile:v", "high", "-level", "4.1",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		outPath,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg export %s: %v\nstderr: %s", video.FilePath(), err, stderr.String())
		http.Error(w, "export failed: "+stderr.String(), http.StatusInternalServerError)
		return
	}

	// Clean up the transcoded file after serving so it does not accumulate
	// in the source directory or appear in a subsequent library sync.
	defer os.Remove(outPath) //nolint:errcheck
	w.Header().Set("Content-Disposition", `attachment; filename="`+outName+`"`)
	http.ServeFile(w, r, outPath)
}

func (s *server) handleSetRating(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	if err := templates.ExecuteTemplate(w, "rating_buttons.html", video); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		log.Printf("ffprobe %s: %v", video.FilePath(), err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	if err := templates.ExecuteTemplate(w, "file_metadata.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleEditMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		log.Printf("ffprobe %s: %v", video.FilePath(), err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	if err := templates.ExecuteTemplate(w, "file_metadata_edit.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
		log.Printf("metadata write %s: %v", video.FilePath(), err)
		// Degrade gracefully: show the unchanged read view rather than a 500.
	}
	// Return the updated read-only view
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		log.Printf("ffprobe %s: %v", video.FilePath(), err)
	}
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	if err := templates.ExecuteTemplate(w, "file_metadata.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "tags.html", tags); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleDirectoryDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var dir store.Directory
	for _, d := range dirs {
		if d.ID == id {
			dir = d
			break
		}
	}
	if dir.ID == 0 {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}
	if err := templates.ExecuteTemplate(w, "directory_delete_confirm.html", dir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleDeleteDirectoryAndFiles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	videos, err := s.store.ListVideosByDirectory(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Explicitly delete each video row and its file on disk.
	// (directory_id is SET NULL on directory delete, not CASCADE, so videos
	// must be removed individually when the caller wants files gone too.)
	for _, v := range videos {
		if err := s.store.DeleteVideo(r.Context(), v.ID); err != nil {
			log.Printf("delete video record %d: %v", v.ID, err)
		}
		if err := os.Remove(v.FilePath()); err != nil {
			log.Printf("delete file %s: %v", v.FilePath(), err)
		}
	}
	if err := s.store.DeleteDirectory(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (s *server) handleLookupModal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	apiKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	data := struct {
		VideoID int64
		HasKey  bool
	}{id, apiKey != ""}
	if err := templates.ExecuteTemplate(w, "lookup_modal.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleLookupSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
		log.Printf("TMDB search %q: %v", q, err)
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
	if err := templates.ExecuteTemplate(w, "lookup_results.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleLookupApply(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
			log.Printf("TMDB movie fetch %s: %v", tmdbID, err)
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
			log.Printf("TMDB series fetch %s: %v", tmdbID, err)
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
		log.Printf("lookup apply metadata write %s: %v", video.FilePath(), err)
		http.Error(w, "metadata write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync the TMDB title into the DB display_name so the library sidebar
	// reflects the new title without requiring a manual re-sync.
	if u.Title != nil && *u.Title != "" {
		if err := s.store.UpdateVideoName(r.Context(), id, *u.Title); err != nil {
			log.Printf("update display_name after TMDB apply %d: %v", id, err)
		}
	}

	// Refresh the metadata view.
	native, _ := metadata.Read(video.FilePath())
	data := struct {
		VideoID int64
		Native  metadata.Meta
	}{id, native}
	if err := templates.ExecuteTemplate(w, "file_metadata.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	autoplay, _ := s.store.GetSetting(r.Context(), "autoplay_random")
	sortOrder, _ := s.store.GetSetting(r.Context(), "video_sort")
	tmdbKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	data := struct {
		AutoplayRandom bool
		VideoSort      string
		TMDBKey        string
	}{
		AutoplayRandom: autoplay != "false",
		VideoSort:      sortOrder,
		TMDBKey:        tmdbKey,
	}
	if err := templates.ExecuteTemplate(w, "settings.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	tmdbKey := strings.TrimSpace(r.FormValue("tmdb_api_key"))
	if err := s.store.SaveSettings(r.Context(), map[string]string{
		"autoplay_random": autoplay,
		"video_sort":      sortOrder,
		"tmdb_api_key":    tmdbKey,
	}); err != nil {
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
	if err := templates.ExecuteTemplate(w, "directories.html", dirs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	s.serveDirList(w, r)
}

func (s *server) handleDirectoryOptions(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "directory_options.html", dirs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleCreateDirectory creates the directory on disk (MkdirAll) then
// registers and syncs it, identical to handleAddDirectory but with the
// extra filesystem creation step.
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
	d, err := s.store.AddDirectory(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.syncDir(d)
	s.serveDirList(w, r)
}

func (s *server) handleAddDirectory(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	d, err := s.store.AddDirectory(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.syncDir(d)
	s.serveDirList(w, r)
}

func (s *server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteDirectory(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.serveDirList(w, r)
}

// --- Filesystem browser ---

// handleBrowseFS lists the immediate visible subdirectories of a path.
// It is used by the folder-picker UI in the library sidebar.
// The path defaults to the user's home directory when not supplied.
func (s *server) handleBrowseFS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		path = home
	}
	path = filepath.Clean(path)

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
	if parent == path { // already at root
		parent = ""
	}

	data := struct {
		Path    string
		Parent  string
		Entries []string
	}{path, parent, dirs}

	if err := templates.ExecuteTemplate(w, "dir_browser.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- P2P sharing ---

func (s *server) handleSharePanel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	if err := templates.ExecuteTemplate(w, "share_panel.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".ogg", ".mov", ".mkv", ".avi":
		return true
	}
	return false
}
