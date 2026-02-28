package main

import (
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed templates/*
var templateFS embed.FS

var (
	videoDir  string
	templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))
)

func main() {
	flag.StringVar(&videoDir, "dir", ".", "directory containing video files")
	port := flag.String("port", "8080", "port to listen on")
	flag.Parse()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", handleIndex)
	r.Get("/videos", handleVideoList)
	r.Get("/play/{filename}", handlePlayer)
	r.Get("/video/{filename}", handleVideoFile)

	log.Printf("Starting server on http://localhost:%s serving videos from: %s", *port, videoDir)
	log.Fatal(http.ListenAndServe(":"+*port, r))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if err := templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleVideoList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		http.Error(w, "could not read video directory", http.StatusInternalServerError)
		return
	}

	var videos []string
	for _, e := range entries {
		if !e.IsDir() && isVideoFile(e.Name()) {
			videos = append(videos, e.Name())
		}
	}

	if err := templates.ExecuteTemplate(w, "video_list.html", videos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handlePlayer(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if err := templates.ExecuteTemplate(w, "player.html", filename); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleVideoFile(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(chi.URLParam(r, "filename"))
	http.ServeFile(w, r, filepath.Join(videoDir, filename))
}

func isVideoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".webm", ".ogg", ".mov", ".mkv", ".avi":
		return true
	}
	return false
}
