package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

//go:embed templates/*
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

type server struct {
	store store.Store
	port  string
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

	srv := &server{store: s, port: *port}

	if *dir != "" {
		d, err := srv.store.AddDirectory(context.Background(), *dir)
		if err != nil {
			log.Printf("warning: could not register dir %s: %v", *dir, err)
		} else {
			srv.syncDir(d)
		}
	}

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

	// Export
	r.Post("/videos/{id}/export/usb", s.handleExportUSB)

	// yt-dlp download
	r.Post("/ytdlp/download", s.handleYTDLPDownload)

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
	filepath.WalkDir(d.Path, func(path string, de fs.DirEntry, err error) error { //nolint:errcheck
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
	})
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"port":      s.port,
		"addresses": addrs,
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
	videos, err := s.store.ListVideos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(videos) == 0 {
		w.Write([]byte(`<p style="color:#444">No videos yet — add a directory to get started.</p>`)) //nolint:errcheck
		return
	}
	video := videos[rand.Intn(len(videos))]
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
	data := struct {
		Videos  []store.Video
		Watched map[int64]bool
	}{videos, watched}
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
	rec, err := s.store.GetWatch(r.Context(), id)
	if err != nil {
		// Not yet watched — return zero position.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"position":0,"watched_at":""}`)) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*60*1e9)
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

	w.Header().Set("Content-Disposition", `attachment; filename="`+outName+`"`)
	http.ServeFile(w, r, outPath)
}

func (s *server) handleSetRating(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
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
	video, err := s.store.GetVideo(r.Context(), id)
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

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	autoplay, _ := s.store.GetSetting(r.Context(), "autoplay_random")
	sortOrder, _ := s.store.GetSetting(r.Context(), "video_sort")
	data := struct {
		AutoplayRandom bool
		VideoSort      string
	}{
		AutoplayRandom: autoplay != "false",
		VideoSort:      sortOrder,
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
	s.store.SetSetting(r.Context(), "autoplay_random", autoplay)   //nolint:errcheck
	s.store.SetSetting(r.Context(), "video_sort", sortOrder)        //nolint:errcheck
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

func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".ogg", ".mov", ".mkv", ".avi":
		return true
	}
	return false
}
