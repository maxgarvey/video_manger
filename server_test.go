package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

func TestConcurrentSync_NoPanic(t *testing.T) {
	root := t.TempDir()
	for i := range 5 {
		name := fmt.Sprintf("vid%d.mp4", i)
		os.WriteFile(filepath.Join(root, name), []byte("fake"), 0644) //nolint:errcheck
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)

	// Call syncDir directly (synchronous) twice back-to-back to verify
	// idempotency under rapid re-invocations.  startSyncDir is async and
	// skips re-entry via syncingDirs, so we exercise the locking/skipping
	// path by calling it twice in quick succession.
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		srv.syncDir(d)
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		srv.syncDir(d)
	}()
	<-done
	<-done

	// DB should have exactly 5 videos (no duplicates, no panics).
	videos, err := srv.store.ListVideosByDirectory(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListVideosByDirectory: %v", err)
	}
	if len(videos) != 5 {
		t.Errorf("expected 5 videos after concurrent sync, got %d", len(videos))
	}
}

func TestHandleVideoList_GroupedByShowAndSeason(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "a.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "b.mp4")
	v3, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "c.mp4")
	// assign shows and seasons
	srv.store.UpdateVideoShowName(ctx, v1.ID, "Foo")
	srv.store.UpdateVideoShowName(ctx, v2.ID, "Foo")
	srv.store.UpdateVideoShowName(ctx, v3.ID, "Bar")
	srv.store.UpdateVideoFields(ctx, v1.ID, store.VideoFields{SeasonNumber: 1})
	srv.store.UpdateVideoFields(ctx, v2.ID, store.VideoFields{SeasonNumber: 2})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Foo") {
		t.Error("expected show group Foo")
	}
	if !strings.Contains(body, "Season 1") || !strings.Contains(body, "Season 2") {
		t.Error("expected season headers for Foo")
	}
	if !strings.Contains(body, "Bar") {
		t.Error("expected show group Bar")
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
