package main

import (
	"context"
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
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

	srv := &server{store: s}

	if *dir != "" {
		d, err := srv.store.AddDirectory(context.Background(), *dir)
		if err != nil {
			log.Printf("warning: could not register dir %s: %v", *dir, err)
		} else {
			srv.syncDir(d)
		}
	}

	log.Printf("Starting server on http://localhost:%s", *port)
	log.Fatal(http.ListenAndServe(":"+*port, srv.routes()))
}

func (s *server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", s.handleIndex)

	// Videos
	r.Get("/videos", s.handleVideoList)
	r.Get("/play/{id}", s.handlePlayer)
	r.Get("/video/{id}", s.handleVideoFile)
	r.Put("/videos/{id}/name", s.handleUpdateVideoName)

	// Tags
	r.Get("/videos/{id}/tags", s.handleVideoTags)
	r.Post("/videos/{id}/tags", s.handleAddVideoTag)
	r.Delete("/videos/{id}/tags/{tagID}", s.handleRemoveVideoTag)
	r.Get("/tags", s.handleListTags)

	// Directories
	r.Get("/directories", s.handleListDirectories)
	r.Post("/directories", s.handleAddDirectory)
	r.Delete("/directories/{id}", s.handleDeleteDirectory)

	return r
}

// syncDir scans a directory on disk and upserts all video files into the store.
// If ffprobe is available, native title is read and used to pre-populate
// display_name for videos that don't yet have one set.
func (s *server) syncDir(d store.Directory) {
	entries, err := os.ReadDir(d.Path)
	if err != nil {
		log.Printf("sync %s: %v", d.Path, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isVideoFile(e.Name()) {
			continue
		}
		v, err := s.store.UpsertVideo(context.Background(), d.ID, e.Name())
		if err != nil {
			log.Printf("upsert %s: %v", e.Name(), err)
			continue
		}
		if v.DisplayName == "" {
			filePath := filepath.Join(d.Path, e.Name())
			if meta, err := metadata.Read(filePath); err == nil && meta.Title != "" {
				if err := s.store.UpdateVideoName(context.Background(), v.ID, meta.Title); err != nil {
					log.Printf("set native title %s: %v", e.Name(), err)
				}
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

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) handleVideoList(w http.ResponseWriter, r *http.Request) {
	var (
		videos []store.Video
		err    error
	)
	if tagStr := r.URL.Query().Get("tag_id"); tagStr != "" {
		tagID, _ := strconv.ParseInt(tagStr, 10, 64)
		videos, err = s.store.ListVideosByTag(r.Context(), tagID)
	} else {
		videos, err = s.store.ListVideos(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "video_list.html", videos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		log.Printf("ffprobe %s: %v", video.FilePath(), err)
	}
	data := struct {
		Video   store.Video
		Tags    []store.Tag
		AllTags []store.Tag
		Native  metadata.Meta
	}{video, tags, allTags, native}
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

func (s *server) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "directories.html", dirs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "directories.html", dirs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := templates.ExecuteTemplate(w, "directories.html", dirs); err != nil {
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
