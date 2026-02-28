package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// --- Unit tests ---

func TestIsVideoFile(t *testing.T) {
	cases := []struct {
		name     string
		want     bool
	}{
		{"movie.mp4", true},
		{"clip.webm", true},
		{"audio.ogg", true},
		{"film.mov", true},
		{"video.mkv", true},
		{"old.avi", true},
		{"UPPER.MP4", true},
		{"Mixed.MkV", true},
		{"document.pdf", false},
		{"image.jpg", false},
		{"script.go", false},
		{"noextension", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVideoFile(tc.name); got != tc.want {
				t.Errorf("isVideoFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// --- Integration tests ---

func newRouter(dir string) http.Handler {
	videoDir = dir
	r := chi.NewRouter()
	r.Get("/", handleIndex)
	r.Get("/videos", handleVideoList)
	r.Get("/play/{filename}", handlePlayer)
	r.Get("/video/{filename}", handleVideoFile)
	return r
}

func TestHandleIndex(t *testing.T) {
	r := newRouter(t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "video_manger") {
		t.Error("expected page title in response body")
	}
	if !strings.Contains(body, "htmx") {
		t.Error("expected htmx script tag in response body")
	}
	if !strings.Contains(body, `hx-get="/videos"`) {
		t.Error("expected htmx video list trigger in response body")
	}
}

func TestHandleVideoList_Empty(t *testing.T) {
	r := newRouter(t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No videos found") {
		t.Error("expected empty state message")
	}
}

func TestHandleVideoList_WithVideos(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.mp4", "beta.mkv", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	r := newRouter(dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "alpha.mp4") {
		t.Error("expected alpha.mp4 in video list")
	}
	if !strings.Contains(body, "beta.mkv") {
		t.Error("expected beta.mkv in video list")
	}
	if strings.Contains(body, "ignore.txt") {
		t.Error("non-video file should not appear in list")
	}
}

func TestHandleVideoList_BadDir(t *testing.T) {
	r := newRouter("/nonexistent/path/that/does/not/exist")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandlePlayer(t *testing.T) {
	r := newRouter(t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/myvideo.mp4", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<video") {
		t.Error("expected <video> element in player response")
	}
	if !strings.Contains(body, "/video/myvideo.mp4") {
		t.Error("expected video src in player response")
	}
}

func TestHandleVideoFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("fake video content")
	if err := os.WriteFile(filepath.Join(dir, "test.mp4"), content, 0644); err != nil {
		t.Fatal(err)
	}

	r := newRouter(dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video/test.mp4", nil)

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != string(content) {
		t.Error("response body does not match file content")
	}
}

func TestHandleVideoFile_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Put a dummy video in dir so the server has something to serve
	r := newRouter(dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video/../../secret.txt", nil)

	r.ServeHTTP(rec, req)

	// filepath.Base strips traversal — the file won't be found outside the dir
	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "secret") {
		t.Error("path traversal should not expose files outside video directory")
	}
}

func TestHandleVideoFile_RangeRequest(t *testing.T) {
	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), content, 0644); err != nil {
		t.Fatal(err)
	}

	r := newRouter(dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video/clip.mp4", nil)
	req.Header.Set("Range", "bytes=0-7")

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content for range request, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "01234567" {
		t.Errorf("expected first 8 bytes, got %q", got)
	}
}
