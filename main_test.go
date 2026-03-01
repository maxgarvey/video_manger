package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	return &server{store: s}
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
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
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
		"title":          {"My Show"},
		"description":    {"A great show"},
		"genre":          {"Drama"},
		"date":           {"2024-01-01"},
		"show":           {"My Show"},
		"network":        {"HBO"},
		"episode_id":     {"S01E01"},
		"season_number":  {"1"},
		"episode_sort":   {"1"},
		"comment":        {""},
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

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
