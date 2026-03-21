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
	"strings"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

func TestHandleConvert_SameExtension(t *testing.T) {
	// mkv-copy on a .mkv source would overwrite the source; expect 400.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mkv")

	form := url.Values{"format": {"mkv-copy"}}
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
	form := url.Values{"format": {"mp4-h264"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleConvert_NoFFmpeg(t *testing.T) {
	// With empty PATH, ffmpeg cannot be found — handler returns 503 immediately.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	t.Setenv("PATH", t.TempDir()) // empty PATH — ffmpeg not found

	form := url.Values{"format": {"mkv-copy"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/convert", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ffmpeg not installed, got %d", rec.Code)
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

	// PATH manipulation so ffmpeg cannot be found — expect 503.
	t.Setenv("PATH", t.TempDir()) // empty PATH: no executables

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/export/usb", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ffmpeg missing, got %d", rec.Code)
	}
}

func TestHandleTrim_InvalidID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/notanid/trim", strings.NewReader("start=0&end=10"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleTrim_VideoNotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/trim", strings.NewReader("start=0&end=10"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleTrim_NoFFmpeg(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	tmp := t.TempDir()
	f := filepath.Join(tmp, "clip.mp4")
	os.WriteFile(f, []byte("fake"), 0644)
	d, _ := srv.store.AddDirectory(ctx, tmp)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, tmp, "clip.mp4")

	t.Setenv("PATH", t.TempDir()) // no ffmpeg

	form := url.Values{"start": {"0"}, "end": {"10"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/videos/%d/trim", v.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when ffmpeg missing, got %d", rec.Code)
	}
}

func TestHandleConvertEvents_JobNotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/convert/events/nonexistent-job-id", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", rec.Code)
	}
}

func TestHandleTrim_Success(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(srcFile, []byte("fake video"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a stub ffmpeg: create the destination file (last arg) and exit 0.
	// Uses only shell builtins so it works even when PATH contains only the stub dir.
	// Args from Trim: ffmpeg -y -ss <start> -to <end> -i <src> -c copy <dst>
	bin := t.TempDir()
	stub := filepath.Join(bin, "ffmpeg")
	stubScript := `#!/bin/sh
# Set $last to the final argument, then create it as an empty file.
for last; do true; done
: > "$last"
`
	if err := os.WriteFile(stub, []byte(stubScript), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	form := url.Values{"start": {"0"}, "end": {"10"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// HX-Trigger must be set with trimComplete payload.
	trigger := rec.Header().Get("HX-Trigger")
	if trigger == "" {
		t.Fatal("expected HX-Trigger header to be set")
	}
	var triggerData map[string]any
	if err := json.Unmarshal([]byte(trigger), &triggerData); err != nil {
		t.Fatalf("HX-Trigger is not valid JSON: %v\nvalue: %s", err, trigger)
	}
	tc, ok := triggerData["trimComplete"]
	if !ok {
		t.Fatalf("HX-Trigger missing trimComplete key: %s", trigger)
	}
	tcMap, ok := tc.(map[string]any)
	if !ok || tcMap["videoId"] == nil {
		t.Errorf("trimComplete.videoId not set: %v", tc)
	}

	// Trimmed file must be in the DB.
	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(videos) < 2 {
		t.Errorf("expected at least 2 videos (original + trimmed), got %d", len(videos))
	}
}

func TestHandleTrim_CopiesVideoFields(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	// Set fields on the source video.
	fields := store.VideoFields{
		Genre:         "Action",
		SeasonNumber:  2,
		EpisodeNumber: 5,
		EpisodeTitle:  "The Pilot",
		Actors:        "Tom Hanks",
		Studio:        "WB",
		Channel:       "HBO",
		AirDate:       "2023-01-01",
	}
	if err := srv.store.UpdateVideoFields(ctx, v.ID, fields); err != nil {
		t.Fatalf("UpdateVideoFields: %v", err)
	}

	form := url.Values{"start": {"0"}, "end": {"5"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Find the trimmed video in the DB.
	videos, err := srv.store.ListVideosByDirectory(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListVideosByDirectory: %v", err)
	}
	if len(videos) < 2 {
		t.Fatalf("expected >= 2 videos (original + trim), got %d", len(videos))
	}
	var trimmed store.Video
	for _, vid := range videos {
		if strings.Contains(vid.Filename, "_trim") {
			trimmed = vid
			break
		}
	}
	if trimmed.ID == 0 {
		t.Fatal("trimmed video not found in DB")
	}

	if trimmed.Genre != "Action" {
		t.Errorf("Genre = %q, want Action", trimmed.Genre)
	}
	if trimmed.SeasonNumber != 2 {
		t.Errorf("SeasonNumber = %d, want 2", trimmed.SeasonNumber)
	}
	if trimmed.EpisodeNumber != 5 {
		t.Errorf("EpisodeNumber = %d, want 5", trimmed.EpisodeNumber)
	}
	if trimmed.EpisodeTitle != "The Pilot" {
		t.Errorf("EpisodeTitle = %q, want 'The Pilot'", trimmed.EpisodeTitle)
	}
	if trimmed.Studio != "WB" {
		t.Errorf("Studio = %q, want WB", trimmed.Studio)
	}
	if trimmed.Channel != "HBO" {
		t.Errorf("Channel = %q, want HBO", trimmed.Channel)
	}
	if trimmed.AirDate != "2023-01-01" {
		t.Errorf("AirDate = %q, want 2023-01-01", trimmed.AirDate)
	}
}

func TestHandleTrim_CopiesDisplayNameWithSuffix(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	// Set a display name on the source video.
	if err := srv.store.UpdateVideoName(ctx, v.ID, "My Great Film"); err != nil {
		t.Fatalf("UpdateVideoName: %v", err)
	}

	form := url.Values{"start": {"0"}, "end": {"5"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	var trimmed store.Video
	for _, vid := range videos {
		if strings.Contains(vid.Filename, "_trim") {
			trimmed = vid
			break
		}
	}
	if trimmed.ID == 0 {
		t.Fatal("trimmed video not found")
	}
	if trimmed.DisplayName != "My Great Film (trim)" {
		t.Errorf("DisplayName = %q, want 'My Great Film (trim)'", trimmed.DisplayName)
	}
}

func TestHandleTrim_CopiesShowName(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ep.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	if err := srv.store.UpdateVideoShowName(ctx, v.ID, "Breaking Bad"); err != nil {
		t.Fatalf("UpdateVideoShowName: %v", err)
	}

	form := url.Values{"start": {"0"}, "end": {"5"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	var trimmed store.Video
	for _, vid := range videos {
		if strings.Contains(vid.Filename, "_trim") {
			trimmed = vid
			break
		}
	}
	if trimmed.ID == 0 {
		t.Fatal("trimmed video not found")
	}
	if trimmed.ShowName != "Breaking Bad" {
		t.Errorf("ShowName = %q, want 'Breaking Bad'", trimmed.ShowName)
	}
}

func TestHandleTrim_CopiesVideoType(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "doc.mp4")

	if err := srv.store.UpdateVideoType(ctx, v.ID, "Movie"); err != nil {
		t.Fatalf("UpdateVideoType: %v", err)
	}

	form := url.Values{"start": {"0"}, "end": {"5"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	var trimmed store.Video
	for _, vid := range videos {
		if strings.Contains(vid.Filename, "_trim") {
			trimmed = vid
			break
		}
	}
	if trimmed.ID == 0 {
		t.Fatal("trimmed video not found")
	}
	if trimmed.VideoType != "Movie" {
		t.Errorf("VideoType = %q, want Movie", trimmed.VideoType)
	}
}

func TestHandleTrim_CopiesUserTagsNotSystemTags(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "doc.mp4")

	// Add a user tag and a system tag (system tag has ":" in its name).
	// The system tag "custom:value" will NOT be one that UpdateVideoFields would create,
	// so we can verify the loop in handleTrim skips copying it.
	userTag, _ := srv.store.UpsertTag(ctx, "favorites")
	systemTag, _ := srv.store.UpsertTag(ctx, "custom:system-tag")
	srv.store.TagVideo(ctx, v.ID, userTag.ID)   //nolint:errcheck
	srv.store.TagVideo(ctx, v.ID, systemTag.ID) //nolint:errcheck

	form := url.Values{"start": {"0"}, "end": {"5"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	var trimmed store.Video
	for _, vid := range videos {
		if strings.Contains(vid.Filename, "_trim") {
			trimmed = vid
			break
		}
	}
	if trimmed.ID == 0 {
		t.Fatal("trimmed video not found")
	}

	trimmedTags, err := srv.store.ListTagsByVideo(ctx, trimmed.ID)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	tagNames := make(map[string]bool)
	for _, tg := range trimmedTags {
		tagNames[tg.Name] = true
	}

	// User tag must be copied.
	if !tagNames["favorites"] {
		t.Error("expected user tag 'favorites' to be copied to trimmed video")
	}
	// System tag (containing ":") must NOT be directly copied via the tag loop.
	if tagNames["custom:system-tag"] {
		t.Error("system tag 'custom:system-tag' should not be directly copied to trimmed video")
	}
}

// 12. handleTrim with no end time (start only — trims to end of file)
func TestHandleTrim_NoEndTime(t *testing.T) {
	bin := makeFfmpegStub(t)
	t.Setenv("PATH", bin)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "long.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "long.mp4")

	// Only start, no end — handler sets end="" which means Trim to EOF.
	form := url.Values{"start": {"30"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/trim", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Trimmed video must appear in DB.
	videos, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(videos) < 2 {
		t.Errorf("expected >= 2 videos after trim-to-end, got %d", len(videos))
	}
}

func TestHandleDelogo_ZeroSize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("fake"), 0644) //nolint:errcheck
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	// w=0 should be rejected (before ffmpeg is even checked in the route).
	// If ffmpeg absent → 503; if ffmpeg present → 400 for zero size.
	form := url.Values{"x": {"0"}, "y": {"0"}, "w": {"0"}, "h": {"0"}, "color": {"0xFFFFFF"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/delogo", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 400 or 503, got %d", rec.Code)
	}
}

func TestHandleConvertEvents_UnknownJob(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/convert/events/nonexistentjob", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing job, got %d", rec.Code)
	}
}

func TestHandleConvertEvents_SendsDoneEvent(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Prepare a finished job with one progress line and a successful outName.
	jobID := "test-done-job"
	ch := make(chan string, 2)
	ch <- "50%"
	close(ch)
	job := &convertJob{ch: ch, outName: "film_converted.mp4"}
	srv.convertJobsMu.Lock()
	srv.convertJobs[jobID] = job
	srv.convertJobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/convert/events/"+jobID, nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "50%") {
		t.Errorf("expected progress line in SSE output, got: %s", body)
	}
	if !strings.Contains(body, "done") {
		t.Errorf("expected done event in SSE output, got: %s", body)
	}
}

func TestHandleConvertEvents_SendsErrorEvent(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	jobID := "test-err-job"
	ch := make(chan string, 1)
	close(ch)
	job := &convertJob{ch: ch, err: fmt.Errorf("ffmpeg failed")}
	srv.convertJobsMu.Lock()
	srv.convertJobs[jobID] = job
	srv.convertJobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/convert/events/"+jobID, nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "error") {
		t.Errorf("expected error event in SSE output, got: %s", body)
	}
}
