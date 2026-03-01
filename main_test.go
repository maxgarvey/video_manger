package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxgarvey/video_manger/store"
	"golang.org/x/crypto/bcrypt"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	return &server{store: s, sessions: make(map[string]time.Time), syncingDirs: make(map[int64]struct{}), convertSem: make(chan struct{}, 2)}
}

// --- Unit tests ---

func TestIsVideoFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
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

func TestHandleIndex(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Video Manger") {
		t.Error("expected title in response body")
	}
	if !strings.Contains(body, `id="player"`) {
		t.Error("expected player element in response body")
	}
	if !strings.Contains(body, `id="lib-btn"`) {
		t.Error("expected library button in response body")
	}
	if !strings.Contains(body, `id="info-btn"`) {
		t.Error("expected info button in response body")
	}
	if !strings.Contains(body, "htmx") {
		t.Error("expected htmx script in response body")
	}
	if !strings.Contains(body, "keydown") {
		t.Error("expected keyboard shortcut listener in response body")
	}
}


func TestHandleVideoList_Empty(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)

	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No videos found") {
		t.Error("expected empty state message")
	}
}

func TestHandleVideoList_WithVideos(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "alpha.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "beta.mkv")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "alpha.mp4") {
		t.Error("expected alpha.mp4 in response")
	}
	if !strings.Contains(body, "beta.mkv") {
		t.Error("expected beta.mkv in response")
	}
}

func TestHandleVideoList_FilterByTag(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "tagged.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "untagged.mp4")
	tag, _ := srv.store.UpsertTag(ctx, "favorites")
	srv.store.TagVideo(ctx, v1.ID, tag.ID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos?tag_id=1", nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "tagged.mp4") {
		t.Error("expected tagged.mp4 in filtered results")
	}
	if strings.Contains(body, "untagged.mp4") {
		t.Error("untagged.mp4 should not appear in filtered results")
	}
}

func TestHandlePlayer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "myvideo.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "myvideo.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/"+itoa(v.ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<video") {
		t.Error("expected <video> element in player response")
	}
}

func TestHandlePlayer_FileNotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "missing.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/"+itoa(v.ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<video") {
		t.Error("should not render <video> element when file is missing")
	}
	if !strings.Contains(body, "File not found") {
		t.Error("expected 'File not found' message in player response")
	}
}

func TestHandlePlayer_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/999", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleVideoFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("fake video content")
	if err := os.WriteFile(filepath.Join(dir, "test.mp4"), content, 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "test.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video/"+itoa(v.ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != string(content) {
		t.Error("response body does not match file content")
	}
}

func TestHandleVideoFile_RangeRequest(t *testing.T) {
	dir := t.TempDir()
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), content, 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/video/"+itoa(v.ID), nil)
	req.Header.Set("Range", "bytes=0-7")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "01234567" {
		t.Errorf("expected first 8 bytes, got %q", got)
	}
}

func TestHandleUpdateVideoName(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "raw.mp4")

	form := url.Values{"name": {"Summer Trip"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/name", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "Summer Trip" {
		t.Errorf("expected response body to be new title, got %q", rec.Body.String())
	}
}

func TestHandleAddAndRemoveVideoTag(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Add tag
	form := url.Values{"tag": {"action"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("add tag: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "action") {
		t.Error("expected tag name in response")
	}

	// Get the tag ID to delete it
	tags, _ := srv.store.ListTagsByVideo(ctx, v.ID)
	if len(tags) == 0 {
		t.Fatal("expected at least one tag")
	}

	// Remove tag
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/tags/"+itoa(tags[0].ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("remove tag: expected 200, got %d", rec.Code)
	}
	remaining, _ := srv.store.ListTagsByVideo(ctx, v.ID)
	if len(remaining) != 0 {
		t.Errorf("expected 0 tags after removal, got %d", len(remaining))
	}
}

func TestHandleInfo(t *testing.T) {
	srv := newTestServer(t)
	srv.port = "8080"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"port"`) {
		t.Error("expected port in /info response")
	}
	if !strings.Contains(body, `"addresses"`) {
		t.Error("expected addresses in /info response")
	}
}

func TestLocalAddresses(t *testing.T) {
	addrs := localAddresses("8080")
	// We can't assert specific IPs in tests, but the function should not panic
	// and all returned values should be valid URLs.
	for _, a := range addrs {
		if !strings.HasPrefix(a, "http://") {
			t.Errorf("expected http:// prefix, got %q", a)
		}
		if !strings.HasSuffix(a, ":8080") {
			t.Errorf("expected :8080 suffix, got %q", a)
		}
	}
}

func TestHandleSettings(t *testing.T) {
	srv := newTestServer(t)

	// GET — returns settings form.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "autoplay_random") {
		t.Error("expected autoplay_random in settings form")
	}

	// POST — save settings.
	form := url.Values{
		"autoplay_random": {"on"},
		"video_sort":      {"rating"},
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST settings: expected 200, got %d", rec.Code)
	}

	ctx := context.Background()
	val, _ := srv.store.GetSetting(ctx, "video_sort")
	if val != "rating" {
		t.Errorf("expected video_sort=rating, got %q", val)
	}
	val, _ = srv.store.GetSetting(ctx, "autoplay_random")
	if val != "true" {
		t.Errorf("expected autoplay_random=true, got %q", val)
	}
}


func TestHandleDirectories(t *testing.T) {
	srv := newTestServer(t)

	// List (empty)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No directories") {
		t.Error("expected empty state message")
	}

	// Add
	form := url.Values{"path": {"/my/videos"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/directories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add dir: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/my/videos") {
		t.Error("expected new directory in response")
	}
}

func TestHandleCreateDirectory_Success(t *testing.T) {
	parent := t.TempDir()
	newDir := filepath.Join(parent, "new_folder")

	srv := newTestServer(t)
	form := url.Values{"path": {newDir}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Dir created on disk.
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("expected directory to be created on disk")
	}
	// Dir registered in DB.
	ctx := context.Background()
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 1 || dirs[0].Path != newDir {
		t.Errorf("expected directory %s in DB, got %+v", newDir, dirs)
	}
}

func TestHandleCreateDirectory_AlreadyExists(t *testing.T) {
	existing := t.TempDir() // already exists
	srv := newTestServer(t)
	form := url.Values{"path": {existing}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("existing dir: expected 200, got %d", rec.Code)
	}
}

func TestHandleCreateDirectory_EmptyPath(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"path": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty path: expected 400, got %d", rec.Code)
	}
}

func TestHandleDirectoryDeleteConfirm(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/my/videos")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/"+itoa(d.ID)+"/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/my/videos") {
		t.Error("expected directory path in confirmation")
	}
	if !strings.Contains(body, "Remove from library") {
		t.Error("expected library-only option")
	}
	if !strings.Contains(body, "Remove and delete files") {
		t.Error("expected file-delete option")
	}
}

func TestHandleDeleteDirectoryAndFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{"ep1.mp4", "ep2.mp4"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep1.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep2.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID)+"/files", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Directory and videos removed from DB
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories, got %d", len(dirs))
	}
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos, got %d", len(videos))
	}
	// Files removed from disk
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted from disk", f)
		}
	}
}

func TestHandleDeleteDirectory(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/to/delete")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories after delete, got %d", len(dirs))
	}
}

func TestHandleDeleteDirectory_KeepsVideos(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/movies")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Library-only remove: DELETE /directories/{id}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Directory is gone.
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories, got %d", len(dirs))
	}

	// Video is still listed with its path intact.
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Fatalf("expected video to survive directory removal, got %d videos", len(videos))
	}
	if videos[0].ID != v.ID {
		t.Errorf("expected video ID %d, got %d", v.ID, videos[0].ID)
	}
	if videos[0].DirectoryPath != "/movies" {
		t.Errorf("expected DirectoryPath=/movies, got %q", videos[0].DirectoryPath)
	}
	if videos[0].FilePath() != "/movies/film.mp4" {
		t.Errorf("expected FilePath=/movies/film.mp4, got %q", videos[0].FilePath())
	}
}

func TestHandleGetMetadata(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/metadata", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleEditMetadata(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/metadata/edit", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="title"`) {
		t.Error("expected title input in edit form")
	}
}

func TestHandleUpdateMetadata(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show.mp4")

	form := url.Values{
		"title":         {"My Show"},
		"description":   {"A great show"},
		"genre":         {"Drama"},
		"date":          {"2024-01-01"},
		"show":          {"My Show"},
		"network":       {"HBO"},
		"episode_id":    {"S01E01"},
		"season_number": {"1"},
		"episode_sort":  {"1"},
		"comment":       {""},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleVideoSearch(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "nature_doc.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "nature_short.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "comedy_special.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos?q=nature", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "nature_doc.mp4") || !strings.Contains(body, "nature_short.mp4") {
		t.Error("expected both nature videos in results")
	}
	if strings.Contains(body, "comedy_special.mp4") {
		t.Error("expected comedy video to be filtered out")
	}
}

func TestHandleProgress(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep1.mp4")

	// GET before any watch — position 0.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET progress (none): expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"position":0`) {
		t.Errorf("expected position:0 for unwatched video, got %s", rec.Body.String())
	}

	// POST progress.
	form := url.Values{"position": {"42.5"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/progress", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST progress: expected 204, got %d", rec.Code)
	}

	// GET after watch — position 42.5.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "42.5") {
		t.Errorf("expected position 42.5, got %s", rec.Body.String())
	}
}

func TestHandleVideoList_ShowsWatchedIndicator(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "watched.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "unwatched.mp4")
	srv.store.RecordWatch(ctx, v1.ID, 10.0)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	// Watched video should show checkmark indicator.
	if !strings.Contains(body, "Watched") {
		t.Error("expected watched indicator in video list")
	}
}

func TestHandleYTDLP_MissingURL(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"dir_id": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleYTDLP_MissingDirID(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"url": {"https://example.com/video"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleYTDLP_InvalidDir(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"url": {"https://example.com/video"}, "dir_id": {"999"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown dir, got %d", rec.Code)
	}
}

func TestHandleYTDLP_NotInstalled(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, t.TempDir())

	t.Setenv("PATH", t.TempDir()) // empty PATH

	form := url.Values{"url": {"https://example.com/v"}, "dir_id": {itoa(d.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when yt-dlp missing, got %d", rec.Code)
	}
}

func TestHandleConvert_SameExtension(t *testing.T) {
	// mkv→mkv (copy preset) would overwrite the source; expect 400.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mkv")

	form := url.Values{"format": {"mkv"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when output == source, got %d", rec.Code)
	}
}

func TestHandleConvert_InvalidFormat(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	form := url.Values{"format": {"avi"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid format, got %d", rec.Code)
	}
}

func TestHandleConvert_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"format": {"mp4"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleConvert_NoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	t.Setenv("PATH", t.TempDir())

	form := url.Values{"format": {"mkv"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ffmpeg missing, got %d", rec.Code)
	}
}

func TestHandleExportUSB_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/export/usb", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleExportUSB_NoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	// PATH manipulation so ffmpeg cannot be found — expect 500.
	t.Setenv("PATH", t.TempDir()) // empty PATH: no executables

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/export/usb", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ffmpeg missing, got %d", rec.Code)
	}
}

func TestHandleSetRating(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")

	setRating := func(rating int) int {
		form := url.Values{"rating": {strconv.Itoa(rating)}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/rating", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)
		return rec.Code
	}

	if code := setRating(1); code != http.StatusOK {
		t.Fatalf("set liked: expected 200, got %d", code)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Rating != 1 {
		t.Errorf("expected rating 1, got %d", got.Rating)
	}

	if code := setRating(2); code != http.StatusOK {
		t.Fatalf("set double-liked: expected 200, got %d", code)
	}
	got, _ = srv.store.GetVideo(ctx, v.ID)
	if got.Rating != 2 {
		t.Errorf("expected rating 2, got %d", got.Rating)
	}

	if code := setRating(0); code != http.StatusOK {
		t.Fatalf("reset rating: expected 200, got %d", code)
	}
	got, _ = srv.store.GetVideo(ctx, v.ID)
	if got.Rating != 0 {
		t.Errorf("expected rating 0, got %d", got.Rating)
	}

	if code := setRating(3); code != http.StatusBadRequest {
		t.Fatalf("invalid rating: expected 400, got %d", code)
	}
}

func TestHandleSetRating_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"rating": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/rating", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleProgress_JSONConsistency(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	// Before any watch — should return JSON with position:0
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var pre map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pre); err != nil {
		t.Fatalf("pre-watch response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if pre["position"] == nil {
		t.Error("expected 'position' key in pre-watch response")
	}

	// After recording a position — should also return valid JSON
	srv.store.RecordWatch(ctx, v.ID, 55.0) //nolint:errcheck
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	var post map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &post); err != nil {
		t.Fatalf("post-watch response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	if post["position"] != 55.0 {
		t.Errorf("expected position 55.0, got %v", post["position"])
	}
}

func TestHandleDeleteVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "gone.mp4")

	// Confirm page
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete-confirm: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gone.mp4") {
		t.Error("expected filename in confirmation")
	}

	// Remove from library only
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE video: expected 200, got %d", rec.Code)
	}
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos after library delete, got %d", len(videos))
	}
}

func TestHandleDeleteVideoAndFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("fake video data")
	filename := "deleteme.mp4"
	if err := os.WriteFile(filepath.Join(dir, filename), content, 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, filename)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/file", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE video/file: expected 200, got %d", rec.Code)
	}
	// DB entry gone
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos after file delete, got %d", len(videos))
	}
	// File gone from disk
	if _, err := os.Stat(filepath.Join(dir, filename)); !os.IsNotExist(err) {
		t.Error("expected file to be deleted from disk")
	}
}

func TestSyncDir_Recursive(t *testing.T) {
	// Build a tree: root/{a.mp4, sub/{b.mkv, ignore.txt}, sub2/{c.mp4}}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	sub2 := filepath.Join(root, "sub2")
	for _, d := range []string{sub, sub2} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mkv"),
		filepath.Join(sub, "ignore.txt"),
		filepath.Join(sub2, "c.mp4"),
	} {
		if err := os.WriteFile(f, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)

	videos, err := srv.store.ListVideos(ctx)
	if err != nil {
		t.Fatalf("ListVideos: %v", err)
	}
	if len(videos) != 3 {
		t.Fatalf("expected 3 videos (a.mp4, b.mkv, c.mp4), got %d", len(videos))
	}

	// Verify FilePath() resolves to correct subdirectory.
	paths := make(map[string]bool)
	for _, v := range videos {
		paths[v.FilePath()] = true
	}
	for _, want := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mkv"),
		filepath.Join(sub2, "c.mp4"),
	} {
		if !paths[want] {
			t.Errorf("expected video at %s, not found in %v", want, paths)
		}
	}
}

func TestSyncDir_AutoTagsByDirectoryName(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mp4"),
	} {
		if err := os.WriteFile(f, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 2 {
		t.Fatalf("expected 2 videos, got %d", len(videos))
	}

	dirTagName := filepath.Base(root)
	for _, v := range videos {
		tags, err := srv.store.ListTagsByVideo(ctx, v.ID)
		if err != nil {
			t.Fatalf("ListTagsByVideo: %v", err)
		}
		found := false
		for _, tag := range tags {
			if tag.Name == dirTagName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("video %s missing auto-tag %q", v.Filename, dirTagName)
		}
	}
}

func TestSyncDir_AutoTag_Idempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)
	srv.syncDir(d) // second sync should not duplicate tags

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(videos))
	}
	tags, _ := srv.store.ListTagsByVideo(ctx, videos[0].ID)
	if len(tags) != 1 {
		t.Errorf("expected exactly 1 tag after double sync, got %d", len(tags))
	}
}

func TestSyncDir_IdempotentOnResync(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)
	srv.syncDir(d) // second sync should not duplicate

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Errorf("expected 1 video after double sync, got %d", len(videos))
	}
}

func TestHandleGetLookupModal_NoKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Settings") {
		t.Error("expected 'Settings' directive in response when no API key configured")
	}
}

func TestHandleLookupSearch_NoKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {"batman"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no API key, got %d", rec.Code)
	}
}

func TestHandleLookupSearch_BadRequest(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", rec.Code)
	}
}

func TestHandleGetLookupModal_WithKey(t *testing.T) {
	// T2: modal with API key set should render the search form.
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Search TMDB") {
		t.Error("expected search form in response when API key is configured")
	}
}

func TestHandleLookupApply_BadMediaType(t *testing.T) {
	// T3: invalid media_type should return 400.
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"media_type": {"book"}, "tmdb_id": {"123"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media_type, got %d", rec.Code)
	}
}

func TestHandleLookupApply_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck

	form := url.Values{"media_type": {"movie"}, "tmdb_id": {"12345"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleSharePanel_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/share", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Body should reference the video's streaming endpoint.
	if !strings.Contains(rec.Body.String(), fmt.Sprintf("/video/%d", v.ID)) {
		t.Error("expected streaming URL with video ID in share panel")
	}
}

func TestHandleSharePanel_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/999/share", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleDirectoryOptions(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Empty — should still return 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/options", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty), got %d", rec.Code)
	}

	// With a directory — it should appear in the options.
	d, _ := srv.store.AddDirectory(ctx, "/my/movies")
	_ = d

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/directories/options", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (with dir), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/my/movies") {
		t.Error("expected directory path in options response")
	}
}

func TestHandleVideoList_RatingSorted(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Set video_sort to "rating"
	srv.store.SaveSettings(ctx, map[string]string{"video_sort": "rating"}) //nolint:errcheck

	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "neutral.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "liked.mp4")
	v3, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "favourite.mp4")

	srv.store.SetVideoRating(ctx, v1.ID, 0) //nolint:errcheck
	srv.store.SetVideoRating(ctx, v2.ID, 1) //nolint:errcheck
	srv.store.SetVideoRating(ctx, v3.ID, 2) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	posNeutral := strings.Index(body, "neutral.mp4")
	posLiked := strings.Index(body, "liked.mp4")
	posFav := strings.Index(body, "favourite.mp4")

	if posFav == -1 || posLiked == -1 || posNeutral == -1 {
		t.Fatal("expected all three videos in response")
	}
	// Higher-rated videos should appear earlier in the HTML.
	if !(posFav < posLiked && posLiked < posNeutral) {
		t.Errorf("expected rating-descending order (fav < liked < neutral), got positions: fav=%d liked=%d neutral=%d",
			posFav, posLiked, posNeutral)
	}
}

// tmdbRoundTripper redirects all requests to a local mock server, allowing
// tests to exercise handleLookupSearch without hitting the real TMDB API.
type tmdbRoundTripper struct {
	host string // e.g. "127.0.0.1:PORT" (no scheme)
}

func (t *tmdbRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = t.host
	return http.DefaultTransport.RoundTrip(r2)
}

func TestHandleLookupSearch_Success(t *testing.T) {
	// Spin up a mock TMDB server.
	mockTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"id":550,"media_type":"movie","title":"Fight Club","release_date":"1999-10-15"}]}`)) //nolint:errcheck
	}))
	defer mockTMDB.Close()

	// Replace the package-level tmdbClient transport so requests go to the mock.
	orig := tmdbClient
	tmdbClient = &http.Client{Transport: &tmdbRoundTripper{host: strings.TrimPrefix(mockTMDB.URL, "http://")}}
	defer func() { tmdbClient = orig }()

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-test-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {"Fight Club"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from TMDB search, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Fight Club") {
		t.Error("expected TMDB result 'Fight Club' in response")
	}
}

func TestHandleVideoList_ShowsLastWatched(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "recent.mp4")
	srv.store.RecordWatch(ctx, v.ID, 10.0) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Should contain a relative timestamp (exact text varies, but the watched
	// indicator and at least one of these strings must appear).
	hasTimestamp := strings.Contains(body, "just now") ||
		strings.Contains(body, "ago") ||
		strings.Contains(body, "yesterday")
	if !hasTimestamp {
		t.Error("expected a relative timestamp for the watched video in the list")
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}

// --- Auth tests ---

func newProtectedServer(t *testing.T, password string) *server {
	t.Helper()
	s := newTestServer(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	s.passwordHash = hash
	return s
}

func TestAuth_NoPassword_PassesThrough(t *testing.T) {
	srv := newTestServer(t) // no password
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 without password, got %d", rec.Code)
	}
}

func TestAuth_WithPassword_UnauthRedirects(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

func TestAuth_LoginPage_Accessible(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /login, got %d", rec.Code)
	}
}

func TestAuth_WrongPassword_RerendersForm(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=wrongpassword")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (re-render) on wrong password, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Wrong password") {
		t.Error("expected 'Wrong password' in response body")
	}
}

func TestAuth_CorrectPassword_SetsSessionCookie(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=secret")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect after correct password, got %d", rec.Code)
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "session=") {
		t.Errorf("expected session cookie in Set-Cookie, got %q", setCookie)
	}
}

func TestAuth_WithSessionCookie_PassesThrough(t *testing.T) {
	srv := newProtectedServer(t, "secret")

	// First login to get a session token.
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=secret")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	// Now request a protected page with the session cookie.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(cookie)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid session, got %d", rec2.Code)
	}
}
