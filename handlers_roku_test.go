package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRokuPoll_EmptyQueue(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/roku/poll", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when queue empty, got %d", rec.Code)
	}
}

func TestHandleRokuCast_And_Poll(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Cast the video.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/roku/cast/"+itoa(v.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cast: expected 204, got %d", rec.Code)
	}

	// Poll should return the video.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/roku/poll", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll: expected 200 with queued video, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "film.mp4") {
		t.Error("expected video in poll response")
	}

	// Second poll should return 204 (queue cleared).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/roku/poll", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second poll: expected 204 (cleared), got %d", rec.Code)
	}
}

func TestHandleRokuConnected_NotPolledRecently(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/roku/connected", nil)
	srv.routes().ServeHTTP(rec, req)
	// No recent poll → 204.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when not recently polled, got %d", rec.Code)
	}
}

func TestHandleRokuConnected_AfterPoll(t *testing.T) {
	srv := newTestServer(t)

	// Trigger a poll so castLastPoll is updated.
	srv.routes().ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/roku/poll", nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/roku/connected", nil)
	srv.routes().ServeHTTP(rec, req)
	// Should be 200 {"connected":true} within the live period.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after recent poll, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "true") {
		t.Error("expected connected:true in response")
	}
}

func TestHandleRokuCast_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/roku/cast/notanid", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad ID, got %d", rec.Code)
	}
}
