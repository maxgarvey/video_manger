package main

import (
	"bytes"
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

	"github.com/maxgarvey/video_manger/store"
)

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

func TestHandleUpdateVideoName_EscapedTitle(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "raw.mp4")

	// Title containing HTML special chars — must be escaped in response.
	form := url.Values{"name": {"<b>Bold & Beautiful</b>"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/name", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<b>") {
		t.Error("response must not contain raw <b> tag (XSS risk)")
	}
	if !strings.Contains(body, "&lt;b&gt;") {
		t.Errorf("expected HTML-escaped title in response, got %q", body)
	}
	// HX-Trigger header must carry both videoRenamed and videoLabelled events.
	hxTrigger := rec.Header().Get("HX-Trigger")
	if !strings.Contains(hxTrigger, "videoRenamed") {
		t.Errorf("HX-Trigger missing videoRenamed, got %q", hxTrigger)
	}
	if !strings.Contains(hxTrigger, "videoLabelled") {
		t.Errorf("HX-Trigger missing videoLabelled, got %q", hxTrigger)
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

func TestHandleDeleteVideoAndFile_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/notanid/file", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
	}
}

func TestHandleDeleteVideoAndFile_FileGone(t *testing.T) {
	// Video is in DB but the file does not exist on disk.
	// os.Remove will fail (slog.Warn), but the handler should still
	// delete the DB record and return 200.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ghost.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/file", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when file is missing, got %d", rec.Code)
	}
	videos, _ := srv.store.ListVideos(ctx)
	for _, vid := range videos {
		if vid.ID == v.ID {
			t.Error("video record should be deleted even if file was missing")
		}
	}
}

func TestHandleDeleteVideo_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/notanid", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
	}
}

func TestHandleVideoDeleteConfirm_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/notanid/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
	}
}

func TestHandleMoveVideo(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Write a real file so Rename has something to move.
	if err := os.WriteFile(filepath.Join(srcDir, "clip.mp4"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "clip.mp4")

	form := url.Values{"dir_id": {itoa(dst.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// File should be at destination.
	if _, err := os.Stat(filepath.Join(dstDir, "clip.mp4")); err != nil {
		t.Errorf("file not found at destination: %v", err)
	}
	// File should be gone from source.
	if _, err := os.Stat(filepath.Join(srcDir, "clip.mp4")); err == nil {
		t.Error("file still exists at source after move")
	}
}

func TestHandleMoveVideo_WithSubdir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "ep1.mp4"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "ep1.mp4")

	form := url.Values{"dir_id": {itoa(dst.ID)}, "subdir": {"Season 1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dstDir, "Season 1", "ep1.mp4")); err != nil {
		t.Errorf("file not found in sub-folder: %v", err)
	}
}

func TestMoveRollback_CrossDevice(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "original.mp4")
	dst := filepath.Join(tmp, "copy.mp4")

	if err := os.WriteFile(src, []byte("video data"), 0644); err != nil {
		t.Fatal(err)
	}
	// Simulate a successful cross-device copy.
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	// Simulate the DB-failure rollback: remove the copy only.
	if err := os.Remove(dst); err != nil {
		t.Fatalf("rollback Remove: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("src was unexpectedly removed during rollback")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("dst still exists after rollback")
	}
}

func TestHandleMoveVideo_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	dst, _ := srv.store.AddDirectory(ctx, t.TempDir())

	form := url.Values{"dir_id": {itoa(dst.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_InvalidDirID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/src")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"dir_id": {"notanint"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid dir_id, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_BadDirectory(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/src")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"dir_id": {"999"}} // nonexistent directory
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent directory, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_SameFile(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "film.mp4"), []byte("data"), 0644) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"dir_id": {itoa(d.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for same src/dst, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_SubdirPathTraversal(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "ep1.mp4"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "ep1.mp4")

	malicious := []string{"../../evil", "../up", "a/b", `a\b`}
	for _, bad := range malicious {
		form := url.Values{"dir_id": {itoa(dst.ID)}, "subdir": {bad}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("subdir=%q: expected 400, got %d", bad, rec.Code)
		}
	}
}

func TestHandleMoveVideo_SubdirTraversal(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "film.mp4"), []byte("data"), 0644) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, srcDir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// subdir with ".." should be rejected
	form := url.Values{"dir_id": {itoa(d.ID)}, "subdir": {"../evil"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal subdir, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_SubdirSlash(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "film.mp4"), []byte("data"), 0644) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, srcDir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// subdir containing "/" should be rejected
	form := url.Values{"dir_id": {itoa(d.ID)}, "subdir": {"a/b"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for slash in subdir, got %d", rec.Code)
	}
}

func TestHandleMoveVideo_Success(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "film.mp4"), []byte("video data"), 0644) //nolint:errcheck

	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "film.mp4")

	form := url.Values{"dir_id": {itoa(dst.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// File should be in dstDir now.
	if _, err := os.Stat(filepath.Join(dstDir, "film.mp4")); err != nil {
		t.Errorf("expected film.mp4 in dstDir after move: %v", err)
	}
	// File should be gone from srcDir.
	if _, err := os.Stat(filepath.Join(srcDir, "film.mp4")); !os.IsNotExist(err) {
		t.Error("expected film.mp4 to be gone from srcDir after move")
	}
}

func TestHandleMoveVideo_SuccessWithSubdir(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "ep.mp4"), []byte("episode"), 0644) //nolint:errcheck

	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "ep.mp4")

	form := url.Values{"dir_id": {itoa(dst.ID)}, "subdir": {"Season1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	subPath := filepath.Join(dstDir, "Season1", "ep.mp4")
	if _, err := os.Stat(subPath); err != nil {
		t.Errorf("expected ep.mp4 in Season1 subdir: %v", err)
	}
}

func TestHandleRenameVideo_RenamesFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "original.mp4")
	os.WriteFile(src, []byte("fake"), 0644) //nolint:errcheck

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, root, "original.mp4")

	body := strings.NewReader(url.Values{"name": {"renamed.mp4"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/rename", v.ID), body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.mp4")); err != nil {
		t.Error("renamed file not found on disk")
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("original file still exists after rename")
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Filename != "renamed.mp4" {
		t.Errorf("DB Filename = %q, want renamed.mp4", got.Filename)
	}
}

func TestHandleRenameVideo_SameName(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "clip.mp4"), []byte("fake"), 0644) //nolint:errcheck

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, root, "clip.mp4")

	body := strings.NewReader(url.Values{"name": {"clip.mp4"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/rename", v.ID), body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// file should still be there unchanged
	if _, err := os.Stat(filepath.Join(root, "clip.mp4")); err != nil {
		t.Error("file disappeared after same-name rename")
	}
}

func TestHandleRenameVideo_EmptyName(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	body := strings.NewReader(url.Values{"name": {""}}.Encode())
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/rename", v.ID), body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRenameVideo_InvalidFilename(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	for _, badName := range []string{"../escape.mp4", "sub/dir.mp4", "back\\slash.mp4"} {
		body := strings.NewReader(url.Values{"name": {badName}}.Encode())
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/rename", v.ID), body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name=%q: expected 400, got %d", badName, rec.Code)
		}
	}
}

func TestHandleSetVideoColor_SetColor(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	body := strings.NewReader(url.Values{"color": {"red"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/color", v.ID), body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.ColorLabel != "red" {
		t.Errorf("ColorLabel = %q, want red", got.ColorLabel)
	}
}

func TestHandleSetVideoColor_UpdateColor(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	for _, color := range []string{"red", "blue"} {
		body := strings.NewReader(url.Values{"color": {color}}.Encode())
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/color", v.ID), body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("color=%s: expected 200, got %d", color, rec.Code)
		}
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.ColorLabel != "blue" {
		t.Errorf("ColorLabel = %q after update, want blue", got.ColorLabel)
	}
}

func TestHandleSetVideoColor_ClearColor(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// set then clear
	for _, color := range []string{"green", ""} {
		body := strings.NewReader(url.Values{"color": {color}}.Encode())
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/color", v.ID), body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("color=%q: expected 200, got %d: %s", color, rec.Code, rec.Body.String())
		}
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.ColorLabel != "" {
		t.Errorf("ColorLabel = %q after clear, want empty", got.ColorLabel)
	}
}

func TestHandleSetVideoColor_InvalidColor(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	body := strings.NewReader(url.Values{"color": {"pink"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/color", v.ID), body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid color") {
		t.Errorf("expected 'invalid color' in response, got: %s", rec.Body.String())
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

func TestHandleVideoSearch_Unicode(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "Résumé.mp4")    //nolint:errcheck
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "日本語ビデオ.mp4")    //nolint:errcheck
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "emoji_🎬.mp4") //nolint:errcheck

	for _, q := range []string{"Résumé", "日本語", "🎬", "café"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/videos?q="+url.QueryEscape(q), nil)
		srv.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("query %q: expected 200, got %d", q, rec.Code)
		}
	}
}

func TestHandleNextUnwatched(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")

	// Empty library — expect 404
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/next-unwatched", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on empty library, got %d", rec.Code)
	}

	// Add a video — should now be returned
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep01.mp4")
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/videos/next-unwatched", nil)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int64(body["id"].(float64)) != v.ID {
		t.Errorf("expected id=%d, got %v", v.ID, body["id"])
	}

	// Mark watched — should be excluded
	srv.store.RecordWatch(ctx, v.ID, 1) //nolint:errcheck
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/videos/next-unwatched", nil)
	srv.routes().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after watching only video, got %d", rec3.Code)
	}
}

func TestHandleListDuplicates(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Create temp dir with a real file so os.Stat succeeds
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "movie.mp4")
	if err := os.WriteFile(f1, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(tmp, "subdir")
	os.MkdirAll(f2, 0755)
	f2 = filepath.Join(f2, "movie.mp4")
	if err := os.WriteFile(f2, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	d1, _ := srv.store.AddDirectory(ctx, tmp)
	d2, _ := srv.store.AddDirectory(ctx, filepath.Dir(f2))
	srv.store.UpsertVideo(ctx, d1.ID, tmp, "movie.mp4")              //nolint:errcheck
	srv.store.UpsertVideo(ctx, d2.ID, filepath.Dir(f2), "movie.mp4") //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/duplicates", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Both files have same name and same size — should appear as duplicates
	if !strings.Contains(rec.Body.String(), "movie.mp4") {
		t.Error("expected duplicate filename in response")
	}
}

func TestHandleMarkWatched_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/watched", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Progress should now be recorded.
	w, err := srv.store.GetWatch(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetWatch: %v", err)
	}
	if w.Position != 1 {
		t.Errorf("expected position 1, got %v", w.Position)
	}
}

func TestHandleMarkWatched(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/watched", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if !got.Watched {
		t.Error("expected video to be marked as watched")
	}
}

func TestHandleMarkWatched_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/notanid/watched", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleClearProgress_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	// Record a watch position first.
	srv.store.RecordWatch(ctx, v.ID, 42.0) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Progress should be gone.
	if _, err := srv.store.GetWatch(ctx, v.ID); err == nil {
		t.Error("expected GetWatch to return error after clearing progress")
	}
}

func TestHandleClearProgress(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")
	srv.store.RecordWatch(ctx, v.ID, 30.0) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleClearProgress_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/badid/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleServeThumbnail_Missing(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// No thumbnail generated — ThumbnailPath is empty.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/thumbnail", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no thumbnail set, got %d", rec.Code)
	}
}

func TestHandleServeThumbnail_NoThumbnailSet(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/thumbnail", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for video with no thumbnail path, got %d", rec.Code)
	}
}

func TestHandleServeThumbnail_WithThumbnail(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Create a real thumb file.
	dir := t.TempDir()
	thumbPath := filepath.Join(dir, "thumb.jpg")
	os.WriteFile(thumbPath, []byte("fake jpeg"), 0644) //nolint:errcheck

	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	srv.store.UpdateVideoThumbnail(ctx, v.ID, thumbPath) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/thumbnail", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for existing thumbnail, got %d", rec.Code)
	}
}

func TestHandleGenerateThumbnail_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/thumbnail", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleGenerateThumbnail_NoFFmpeg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	t.Setenv("PATH", t.TempDir()) // no ffmpeg

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/thumbnail", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ffmpeg missing, got %d", rec.Code)
	}
}

func TestHandleGenerateThumbnail_WithPositionParam(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "film.mp4"), []byte("fake"), 0644) //nolint:errcheck
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Test with a position query param — exercises the parsing branch.
	// If ffmpeg is absent, GenerateThumbnail will fail → 500 or 503.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/thumbnail?position=0.5", nil)
	srv.routes().ServeHTTP(rec, req)
	// Accept either success (ffmpeg present) or failure (ffmpeg absent).
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", rec.Code)
	}
}

func TestHandleVideoTags(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Add a tag directly through the store.
	tag, _ := srv.store.UpsertTag(ctx, "drama")
	srv.store.TagVideo(ctx, v.ID, tag.ID) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/tags", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "drama") {
		t.Error("expected tag name in video tags response")
	}
}

func TestHandleVideoTags_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/9999/tags", nil)
	srv.routes().ServeHTTP(rec, req)
	// Handler calls parseIDParam which is OK, then calls store.ListTagsByVideo
	// which succeeds on a nonexistent video (just returns empty slice), so 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for nonexistent video tags, got %d", rec.Code)
	}
}

func TestHandleVideoTags_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/notanid/tags", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
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

func TestSRTToWebVTT_AddsHeader(t *testing.T) {
	vtt := srtToWebVTT("")
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Errorf("expected WEBVTT header, got: %q", vtt)
	}
}

func TestSRTToWebVTT_ConvertTimestamps(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:04,500\nHello world\n"
	vtt := srtToWebVTT(srt)
	if strings.Contains(vtt, "00:00:01,000") {
		t.Error("expected comma replaced with dot in timestamp")
	}
	if !strings.Contains(vtt, "00:00:01.000 --> 00:00:04.500") {
		t.Errorf("expected dot-separated timestamp in output, got:\n%s", vtt)
	}
	if !strings.Contains(vtt, "Hello world") {
		t.Error("expected subtitle text to be preserved")
	}
}

func TestSRTToWebVTT_DoesNotAlterTextCommas(t *testing.T) {
	// Commas inside dialogue lines must not be touched.
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHello, world!\n"
	vtt := srtToWebVTT(srt)
	if !strings.Contains(vtt, "Hello, world!") {
		t.Error("dialogue comma should not be removed")
	}
}

func TestSRTToWebVTT_MultipleEntries(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:02,000\nFirst\n\n2\n00:00:03,000 --> 00:00:04,000\nSecond\n"
	vtt := srtToWebVTT(srt)
	if !strings.Contains(vtt, "First") || !strings.Contains(vtt, "Second") {
		t.Error("expected both subtitle entries in output")
	}
}

func TestHandleServeSubtitles_NotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, dir)
	// Write a video file but NO .srt
	f := filepath.Join(dir, "movie.mp4")
	os.WriteFile(f, []byte("fake"), 0o644)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/videos/%d/subtitles", v.ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when no .srt exists, got %d", rec.Code)
	}
}

func TestHandleServeSubtitles_ServesWebVTT(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, dir)

	// Write video stub + .srt file.
	os.WriteFile(filepath.Join(dir, "film.mp4"), []byte("fake"), 0o644)
	srtContent := "1\n00:00:01,000 --> 00:00:02,000\nHello!\n"
	os.WriteFile(filepath.Join(dir, "film.srt"), []byte(srtContent), 0o644)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/videos/%d/subtitles", v.ID), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/vtt") {
		t.Errorf("expected text/vtt content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "WEBVTT") {
		t.Errorf("expected WEBVTT header in response body")
	}
	if !strings.Contains(body, "Hello!") {
		t.Error("expected subtitle text in response")
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

func TestHandlePostProgress_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/notanid/progress", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
	}
}

func TestHandleRandomVideoID_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/random-video", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int64(body["id"].(float64)) != v.ID {
		t.Errorf("expected id=%d, got %v", v.ID, body["id"])
	}
}

func TestHandleRandomVideoID_Empty(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/random-video", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on empty library, got %d", rec.Code)
	}
}

func TestHandleLogout_ClearsCookie(t *testing.T) {
	srv := newTestServerWithAuth(t, "secret")

	// First: log in to get a session cookie.
	loginBody := strings.NewReader(url.Values{"password": {"secret"}}.Encode())
	loginReq := httptest.NewRequest(http.MethodPost, "/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	srv.routes().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("login: expected 302, got %d", loginRec.Code)
	}
	cookieHeader := loginRec.Header().Get("Set-Cookie")
	if cookieHeader == "" {
		t.Fatal("login: expected session cookie")
	}
	// Extract the token value from "session=TOKEN; ..."
	var token string
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "session=") {
			token = strings.TrimPrefix(part, "session=")
		}
	}
	if token == "" {
		t.Fatal("could not extract session token from cookie")
	}

	// Logout with the session cookie.
	logoutReq := httptest.NewRequest(http.MethodGet, "/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "session", Value: token})
	logoutRec := httptest.NewRecorder()
	srv.routes().ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusFound {
		t.Fatalf("logout: expected redirect, got %d", logoutRec.Code)
	}
	// The session should no longer be valid.
	srv.sessionsMu.RLock()
	_, exists := srv.sessions[token]
	srv.sessionsMu.RUnlock()
	if exists {
		t.Error("session should be removed from in-memory map after logout")
	}
}

func TestHandleLogout_NoCookieIsHarmless(t *testing.T) {
	srv := newTestServerWithAuth(t, "secret")

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	// Should redirect to /login without panicking.
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect from logout, got %d", rec.Code)
	}
}

func TestHandleCopyToLibrary_OK(t *testing.T) {
	srcDir := t.TempDir()
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "clip.mp4"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"library_path": libDir}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, srcDir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/copy-to-library", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(libDir, "clip.mp4")); err != nil {
		t.Errorf("expected clip.mp4 in library dir: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "Copied") {
		t.Error("expected 'Copied' confirmation in response")
	}
}

func TestHandleCopyToLibrary_NoLibraryPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/copy-to-library", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when library_path not configured, got %d", rec.Code)
	}
}

func TestHandleCopyToLibrary_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"library_path": t.TempDir()}) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/copy-to-library", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleCopyToLibrary_SourceMissing(t *testing.T) {
	libDir := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"library_path": libDir}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ghost.mp4") // no file on disk

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/copy-to-library", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when source file missing, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_OK(t *testing.T) {
	dir := t.TempDir()
	newFile := filepath.Join(dir, "moved.mp4")
	if err := os.WriteFile(newFile, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "original.mp4")

	form := url.Values{"newpath": {newFile}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Filename != "moved.mp4" {
		t.Errorf("Filename = %q, want moved.mp4", got.Filename)
	}
}

func TestHandleRelocateVideo_MissingNewpath(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when newpath missing, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_FileNotOnDisk(t *testing.T) {
	// handleRelocateVideo checks os.Stat before any DB lookup, so a
	// path that does not exist on disk should return 400.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"newpath": {"/nonexistent/ghost.mp4"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when file not on disk, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_OutsideLibrary(t *testing.T) {
	// File exists on disk but is not inside any registered library directory.
	dir := t.TempDir()
	newFile := filepath.Join(dir, "orphan.mp4")
	if err := os.WriteFile(newFile, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	// Register a different directory, not dir.
	otherDir := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, otherDir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"newpath": {newFile}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when path is outside library, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_MissingPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_FileNotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"newpath": {"/nonexistent/path/film.mp4"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/relocate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent file, got %d", rec.Code)
	}
}

func TestHandleRelocateVideo_BadID(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"newpath": {"/some/path/film.mp4"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/notanid/relocate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
	}
}

func TestCopyFile_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "dst.mp4")
	content := []byte("video content")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestCopyFile_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nonexistent.mp4"), filepath.Join(dir, "dst.mp4"))
	if err == nil {
		t.Error("expected error for missing source, got nil")
	}
}

func TestOpenFreeFile_FirstName(t *testing.T) {
	dir := t.TempDir()
	f, name, err := openFreeFile(dir, "clip", ".mp4")
	if err != nil {
		t.Fatalf("openFreeFile: %v", err)
	}
	f.Close()
	if name != "clip.mp4" {
		t.Errorf("expected clip.mp4, got %q", name)
	}
}

func TestOpenFreeFile_Collision(t *testing.T) {
	dir := t.TempDir()
	// Pre-create the base name so a suffix is needed.
	os.WriteFile(filepath.Join(dir, "clip.mp4"), nil, 0644) //nolint:errcheck

	f, name, err := openFreeFile(dir, "clip", ".mp4")
	if err != nil {
		t.Fatalf("openFreeFile with collision: %v", err)
	}
	f.Close()
	if name != "clip_2.mp4" {
		t.Errorf("expected clip_2.mp4 after collision, got %q", name)
	}
}

func TestFreeOutputName_NoneExist(t *testing.T) {
	dir := t.TempDir()
	name := freeOutputName(dir, "clip", "_trim", ".mp4")
	if name != "clip_trim.mp4" {
		t.Errorf("expected clip_trim.mp4, got %q", name)
	}
}

func TestFreeOutputName_FirstTaken(t *testing.T) {
	dir := t.TempDir()
	// Create the first candidate so freeOutputName must increment.
	if err := os.WriteFile(filepath.Join(dir, "clip_trim.mp4"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	name := freeOutputName(dir, "clip", "_trim", ".mp4")
	if name != "clip_trim_2.mp4" {
		t.Errorf("expected clip_trim_2.mp4, got %q", name)
	}
}

func TestFreeOutputName_MultipleCollisions(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"out.mp4", "out_2.mp4", "out_3.mp4"} {
		os.WriteFile(filepath.Join(dir, n), []byte{}, 0644) //nolint:errcheck
	}
	name := freeOutputName(dir, "out", "", ".mp4")
	if name != "out_4.mp4" {
		t.Errorf("expected out_4.mp4, got %q", name)
	}
}

func TestFindRegisteredDir_ExactMatch(t *testing.T) {
	dirs := []store.Directory{
		{ID: 1, Path: "/lib/movies"},
		{ID: 2, Path: "/lib/shows"},
	}
	id, under := findRegisteredDir(dirs, "/lib/movies")
	if id != 1 {
		t.Errorf("expected dirID 1, got %d", id)
	}
	if !under {
		t.Error("expected underLib=true")
	}
}

func TestFindRegisteredDir_Subdirectory(t *testing.T) {
	dirs := []store.Directory{{ID: 1, Path: "/lib/movies"}}
	id, under := findRegisteredDir(dirs, "/lib/movies/action")
	if id != 0 {
		t.Errorf("expected dirID 0 for subdir, got %d", id)
	}
	if !under {
		t.Error("expected underLib=true for subdir")
	}
}

func TestFindRegisteredDir_OutsideLibrary(t *testing.T) {
	dirs := []store.Directory{{ID: 1, Path: "/lib/movies"}}
	id, under := findRegisteredDir(dirs, "/external/stuff")
	if id != 0 || under {
		t.Errorf("expected (0, false) for outside library, got (%d, %v)", id, under)
	}
}

func TestFindRegisteredDir_NoPartialPrefixMatch(t *testing.T) {
	// "/lib/mov" must NOT match "/lib/movies" (no separator between them).
	dirs := []store.Directory{{ID: 1, Path: "/lib/movies"}}
	_, under := findRegisteredDir(dirs, "/lib/mov")
	if under {
		t.Error("expected no match for partial prefix without path separator")
	}
}

func TestParseEpisodeID(t *testing.T) {
	cases := []struct {
		in    string
		wantS int
		wantE int
	}{
		{"S02E05", 2, 5},
		{"s02e05", 2, 5},
		{"S01E01", 1, 1},
		{"S10E22", 10, 22},
		{"", 0, 0},
		{"nopattern", 0, 0},
		{"S00", 0, 0},    // no E
		{"E05", 0, 0},    // no S
		{"SxxExx", 0, 0}, // non-numeric
	}
	for _, c := range cases {
		s, e := parseEpisodeID(c.in)
		if s != c.wantS || e != c.wantE {
			t.Errorf("parseEpisodeID(%q) = (%d,%d), want (%d,%d)", c.in, s, e, c.wantS, c.wantE)
		}
	}
}

// flusherRecorder is an httptest.ResponseRecorder that also implements http.Flusher.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() { f.flushed = true }

func TestNewSSEWriter_SetsHeaders(t *testing.T) {
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw, ok := newSSEWriter(rec)
	if !ok {
		t.Fatal("expected newSSEWriter to succeed with a Flusher")
	}
	if sw == nil {
		t.Fatal("expected non-nil sseWriter")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", cc)
	}
}

// noFlushWriter is a ResponseWriter that deliberately does NOT implement Flusher.
type noFlushWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *noFlushWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *noFlushWriter) WriteHeader(code int)        { w.code = code }

func TestNewSSEWriter_NoFlusher(t *testing.T) {
	nfw := &noFlushWriter{}
	sw, ok := newSSEWriter(nfw)
	if ok {
		t.Error("expected newSSEWriter to fail for non-Flusher")
	}
	if sw != nil {
		t.Error("expected nil sseWriter on failure")
	}
	if nfw.code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", nfw.code)
	}
}

func TestSSEWriter_Data(t *testing.T) {
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw, _ := newSSEWriter(rec)
	sw.Data("hello world")
	body := rec.Body.String()
	if !strings.Contains(body, "data: hello world\n\n") {
		t.Errorf("unexpected SSE Data output: %q", body)
	}
	if !rec.flushed {
		t.Error("expected Flush to be called after Data")
	}
}

func TestSSEWriter_Data_StripNewlines(t *testing.T) {
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw, _ := newSSEWriter(rec)
	sw.Data("line1\nline2\r\nline3")
	body := rec.Body.String()
	if strings.Contains(body, "\nline2") || strings.Contains(body, "\nline3") {
		t.Errorf("Data should strip embedded newlines, got: %q", body)
	}
}

func TestSSEWriter_Event(t *testing.T) {
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	sw, _ := newSSEWriter(rec)
	sw.Event("progress", "50%")
	body := rec.Body.String()
	if !strings.Contains(body, "event: progress\n") {
		t.Errorf("expected event line in output, got: %q", body)
	}
	if !strings.Contains(body, "data: 50%\n\n") {
		t.Errorf("expected data line in output, got: %q", body)
	}
}

// ── Thumbnail move tests ──────────────────────────────────────────────────────

func TestHandleMoveVideo_MovesThumbWithVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Write a fake video file and a fake thumbnail.
	os.WriteFile(filepath.Join(srcDir, "film.mp4"), []byte("video data"), 0644) //nolint:errcheck
	thumbSrc := filepath.Join(srcDir, "film_thumb.jpg")
	os.WriteFile(thumbSrc, []byte("fake jpeg"), 0644) //nolint:errcheck

	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "film.mp4")
	srv.store.UpdateVideoThumbnail(ctx, v.ID, thumbSrc) //nolint:errcheck

	form := url.Values{"dir_id": {itoa(dst.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	thumbDst := filepath.Join(dstDir, "film_thumb.jpg")

	// Thumbnail must be at the new location.
	if _, err := os.Stat(thumbDst); err != nil {
		t.Errorf("expected thumbnail at dstDir after move: %v", err)
	}
	// Original thumbnail must be gone.
	if _, err := os.Stat(thumbSrc); !os.IsNotExist(err) {
		t.Error("expected original thumbnail to be gone from srcDir")
	}

	// DB thumbnail_path must reflect new path.
	got, err := srv.store.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.ThumbnailPath != thumbDst {
		t.Errorf("DB thumbnail_path = %q, want %q", got.ThumbnailPath, thumbDst)
	}

	// Serving the thumbnail endpoint must return 200.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/thumbnail", nil)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 from /thumbnail after move, got %d", rec2.Code)
	}
}

func TestHandleMoveVideo_ThumbMissingOnDisk(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "film.mp4"), []byte("video data"), 0644) //nolint:errcheck

	src, _ := srv.store.AddDirectory(ctx, srcDir)
	dst, _ := srv.store.AddDirectory(ctx, dstDir)
	v, _ := srv.store.UpsertVideo(ctx, src.ID, src.Path, "film.mp4")

	// DB has a stale thumbnail path that doesn't exist on disk.
	stalePath := filepath.Join(srcDir, "stale_thumb.jpg")
	srv.store.UpdateVideoThumbnail(ctx, v.ID, stalePath) //nolint:errcheck

	form := url.Values{"dir_id": {itoa(dst.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/move", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	// Move must succeed even though the thumbnail file is missing.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with missing thumb, got %d: %s", rec.Code, rec.Body.String())
	}
	// Video file moved successfully.
	if _, err := os.Stat(filepath.Join(dstDir, "film.mp4")); err != nil {
		t.Errorf("expected film.mp4 in dstDir: %v", err)
	}
}
