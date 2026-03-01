package store_test

import (
	"context"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	return s
}

// --- Directory tests ---

func TestAddAndListDirectories(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, err := s.AddDirectory(ctx, "/videos/movies")
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	if d.Path != "/videos/movies" {
		t.Errorf("got path %q, want /videos/movies", d.Path)
	}

	dirs, err := s.ListDirectories(ctx)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	if len(dirs) != 1 || dirs[0].ID != d.ID {
		t.Errorf("expected 1 directory with id %d, got %+v", d.ID, dirs)
	}
}

func TestDeleteDirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos/tmp")
	if err := s.DeleteDirectory(ctx, d.ID); err != nil {
		t.Fatalf("DeleteDirectory: %v", err)
	}
	dirs, _ := s.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories after delete, got %d", len(dirs))
	}
}

func TestDeleteDirectory_CascadesVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, "clip.mp4")
	s.DeleteDirectory(ctx, d.ID)

	videos, _ := s.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected videos to be cascade-deleted, got %d", len(videos))
	}
}

// --- Video tests ---

func TestUpsertVideo_Idempotentent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, err := s.UpsertVideo(ctx, d.ID, "movie.mp4")
	if err != nil {
		t.Fatalf("UpsertVideo first: %v", err)
	}
	v2, err := s.UpsertVideo(ctx, d.ID, "movie.mp4")
	if err != nil {
		t.Fatalf("UpsertVideo second: %v", err)
	}
	if v1.ID != v2.ID {
		t.Errorf("expected same ID on upsert, got %d and %d", v1.ID, v2.ID)
	}
}

func TestListVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, "alpha.mp4")
	s.UpsertVideo(ctx, d.ID, "beta.mkv")

	videos, err := s.ListVideos(ctx)
	if err != nil {
		t.Fatalf("ListVideos: %v", err)
	}
	if len(videos) != 2 {
		t.Fatalf("expected 2 videos, got %d", len(videos))
	}
	if videos[0].DirectoryPath != "/videos" {
		t.Errorf("expected DirectoryPath /videos, got %q", videos[0].DirectoryPath)
	}
}

func TestGetVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "film.mp4")

	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.Filename != "film.mp4" || got.DirectoryPath != "/videos" {
		t.Errorf("unexpected video: %+v", got)
	}
}

func TestUpdateVideoName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "raw_footage.mp4")

	if err := s.UpdateVideoName(ctx, v.ID, "Summer Trip"); err != nil {
		t.Fatalf("UpdateVideoName: %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.Title() != "Summer Trip" {
		t.Errorf("expected Title()=Summer Trip, got %q", got.Title())
	}
}

func TestDeleteVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "to_delete.mp4")

	if err := s.DeleteVideo(ctx, v.ID); err != nil {
		t.Fatalf("DeleteVideo: %v", err)
	}
	videos, _ := s.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos after delete, got %d", len(videos))
	}
	if _, err := s.GetVideo(ctx, v.ID); err == nil {
		t.Error("expected error getting deleted video, got nil")
	}
}

func TestVideoTitle_FallsBackToFilename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "untitled.mp4")

	got, _ := s.GetVideo(ctx, v.ID)
	if got.Title() != "untitled.mp4" {
		t.Errorf("expected Title() to fall back to filename, got %q", got.Title())
	}
}

// --- Tag tests ---

func TestUpsertTag_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t1, err := s.UpsertTag(ctx, "action")
	if err != nil {
		t.Fatalf("UpsertTag: %v", err)
	}
	t2, err := s.UpsertTag(ctx, "action")
	if err != nil {
		t.Fatalf("UpsertTag second: %v", err)
	}
	if t1.ID != t2.ID {
		t.Errorf("expected same tag ID on upsert, got %d and %d", t1.ID, t2.ID)
	}
}

func TestTagAndUntagVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "film.mp4")
	tag, _ := s.UpsertTag(ctx, "comedy")

	if err := s.TagVideo(ctx, v.ID, tag.ID); err != nil {
		t.Fatalf("TagVideo: %v", err)
	}
	tags, err := s.ListTagsByVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "comedy" {
		t.Errorf("expected [comedy], got %+v", tags)
	}

	if err := s.UntagVideo(ctx, v.ID, tag.ID); err != nil {
		t.Fatalf("UntagVideo: %v", err)
	}
	tags, _ = s.ListTagsByVideo(ctx, v.ID)
	if len(tags) != 0 {
		t.Errorf("expected no tags after untag, got %+v", tags)
	}
}

func TestListVideosByTag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, "alpha.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, "beta.mp4")
	tag, _ := s.UpsertTag(ctx, "favorites")

	s.TagVideo(ctx, v1.ID, tag.ID)

	videos, err := s.ListVideosByTag(ctx, tag.ID)
	if err != nil {
		t.Fatalf("ListVideosByTag: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != v1.ID {
		t.Errorf("expected only alpha.mp4 tagged, got %+v", videos)
	}
	_ = v2
}

func TestTagVideo_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, "film.mp4")
	tag, _ := s.UpsertTag(ctx, "drama")

	s.TagVideo(ctx, v.ID, tag.ID)
	if err := s.TagVideo(ctx, v.ID, tag.ID); err != nil {
		t.Errorf("duplicate TagVideo should not error: %v", err)
	}
	tags, _ := s.ListTagsByVideo(ctx, v.ID)
	if len(tags) != 1 {
		t.Errorf("expected exactly 1 tag, got %d", len(tags))
	}
}
