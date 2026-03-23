package store_test

import (
	"context"
	"testing"
	"time"

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

func TestDeleteDirectory_OrphansVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")
	s.DeleteDirectory(ctx, d.ID)

	// Videos survive directory removal; directory_id becomes NULL.
	videos, _ := s.ListVideos(ctx)
	if len(videos) != 1 {
		t.Fatalf("expected video to survive directory deletion, got %d videos", len(videos))
	}
	if videos[0].DirectoryID != 0 {
		t.Errorf("expected DirectoryID=0 (orphaned), got %d", videos[0].DirectoryID)
	}
	if videos[0].DirectoryPath != "/videos" {
		t.Errorf("expected DirectoryPath=/videos, got %q", videos[0].DirectoryPath)
	}
}

// --- Video tests ---

func TestUpsertVideo_Idempotentent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, err := s.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")
	if err != nil {
		t.Fatalf("UpsertVideo first: %v", err)
	}
	v2, err := s.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")
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
	s.UpsertVideo(ctx, d.ID, d.Path, "alpha.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "beta.mkv")

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
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

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
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "raw_footage.mp4")

	if err := s.UpdateVideoName(ctx, v.ID, "Summer Trip"); err != nil {
		t.Fatalf("UpdateVideoName: %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.Title() != "Summer Trip" {
		t.Errorf("expected Title()=Summer Trip, got %q", got.Title())
	}
}

func TestListVideosByDirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d1, _ := s.AddDirectory(ctx, "/videos/a")
	d2, _ := s.AddDirectory(ctx, "/videos/b")
	s.UpsertVideo(ctx, d1.ID, d1.Path, "one.mp4")
	s.UpsertVideo(ctx, d1.ID, d1.Path, "two.mp4")
	s.UpsertVideo(ctx, d2.ID, d2.Path, "three.mp4")

	vids, err := s.ListVideosByDirectory(ctx, d1.ID)
	if err != nil {
		t.Fatalf("ListVideosByDirectory: %v", err)
	}
	if len(vids) != 2 {
		t.Errorf("expected 2 videos for d1, got %d", len(vids))
	}
	for _, v := range vids {
		if v.DirectoryID != d1.ID {
			t.Errorf("expected directory_id %d, got %d", d1.ID, v.DirectoryID)
		}
	}

	vids2, _ := s.ListVideosByDirectory(ctx, d2.ID)
	if len(vids2) != 1 {
		t.Errorf("expected 1 video for d2, got %d", len(vids2))
	}
}

func TestSearchVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, d.Path, "bobs_burgers_s01e01.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "bobs_burgers_s01e02.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "archer_s01e01.mp4")

	results, err := s.SearchVideos(ctx, "bobs")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'bobs', got %d", len(results))
	}

	results, err = s.SearchVideos(ctx, "ARCHER")
	if err != nil {
		t.Fatalf("SearchVideos case-insensitive: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'ARCHER', got %d", len(results))
	}

	results, err = s.SearchVideos(ctx, "nomatch")
	if err != nil {
		t.Fatalf("SearchVideos no match: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'nomatch', got %d", len(results))
	}
}

func TestGetRandomVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty library — should return an error.
	if _, err := s.GetRandomVideo(ctx); err == nil {
		t.Error("expected error from GetRandomVideo on empty store, got nil")
	}

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, d.Path, "a.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "b.mp4")

	v, err := s.GetRandomVideo(ctx)
	if err != nil {
		t.Fatalf("GetRandomVideo: %v", err)
	}
	if v.Filename != "a.mp4" && v.Filename != "b.mp4" {
		t.Errorf("unexpected filename from GetRandomVideo: %q", v.Filename)
	}
}

func TestSaveSettings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveSettings(ctx, map[string]string{
		"autoplay_random": "false",
		"video_sort":      "rating",
		"tmdb_api_key":    "tok123",
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	for key, want := range map[string]string{
		"autoplay_random": "false",
		"video_sort":      "rating",
		"tmdb_api_key":    "tok123",
	} {
		got, err := s.GetSetting(ctx, key)
		if err != nil {
			t.Fatalf("GetSetting(%q): %v", key, err)
		}
		if got != want {
			t.Errorf("GetSetting(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestDeleteVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "to_delete.mp4")

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
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "untitled.mp4")

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
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
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
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "alpha.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "beta.mp4")
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

func TestPruneOrphanTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	tag, _ := s.UpsertTag(ctx, "drama")
	s.TagVideo(ctx, v.ID, tag.ID) //nolint:errcheck

	// Remove the only association; tag should now be orphaned.
	s.UntagVideo(ctx, v.ID, tag.ID) //nolint:errcheck
	if err := s.PruneOrphanTags(ctx); err != nil {
		t.Fatalf("PruneOrphanTags: %v", err)
	}
	tags, _ := s.ListTags(ctx)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after prune, got %d: %+v", len(tags), tags)
	}
}

func TestPruneOrphanTags_KeepsUsed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	used, _ := s.UpsertTag(ctx, "comedy")
	orphan, _ := s.UpsertTag(ctx, "unused")
	s.TagVideo(ctx, v.ID, used.ID) //nolint:errcheck
	_ = orphan

	if err := s.PruneOrphanTags(ctx); err != nil {
		t.Fatalf("PruneOrphanTags: %v", err)
	}
	tags, _ := s.ListTags(ctx)
	if len(tags) != 1 || tags[0].Name != "comedy" {
		t.Errorf("expected only 'comedy' tag to survive, got %+v", tags)
	}
}

func TestSearchVideos_LikeWildcardEscaping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, d.Path, "action_film.mp4") //nolint:errcheck
	s.UpsertVideo(ctx, d.ID, d.Path, "comedy.mp4")      //nolint:errcheck

	// A literal "%" should not match all rows.
	results, err := s.SearchVideos(ctx, "%")
	if err != nil {
		t.Fatalf("SearchVideos(%%): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for literal %% query, got %d", len(results))
	}

	// A literal "_" should not act as a wildcard (matching everything);
	// it should match only files whose name contains a literal underscore.
	results, err = s.SearchVideos(ctx, "_")
	if err != nil {
		t.Fatalf("SearchVideos(_): %v", err)
	}
	// action_film.mp4 contains a literal '_'; comedy.mp4 does not.
	if len(results) != 1 {
		t.Errorf("expected 1 result for literal _ query (only file with underscore), got %d", len(results))
	}

	// Normal term should still work.
	results, err = s.SearchVideos(ctx, "action")
	if err != nil {
		t.Fatalf("SearchVideos(action): %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'action', got %d", len(results))
	}
}

func TestTagVideo_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
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

// --- T11: GetDirectory ---

func TestGetDirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, err := s.AddDirectory(ctx, "/my/videos")
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}

	got, err := s.GetDirectory(ctx, d.ID)
	if err != nil {
		t.Fatalf("GetDirectory: %v", err)
	}
	if got.ID != d.ID || got.Path != "/my/videos" {
		t.Errorf("GetDirectory returned unexpected value: %+v", got)
	}

	// Non-existent ID should return an error.
	if _, err := s.GetDirectory(ctx, 9999); err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

// --- T10: GetSetting / SaveSettings ---

func TestGetAndSetSetting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Default (no row yet) returns empty string, not an error.
	val, err := s.GetSetting(ctx, "missing_key")
	if err != nil {
		t.Fatalf("GetSetting for missing key: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}

	// SaveSettings creates the row.
	if err := s.SaveSettings(ctx, map[string]string{"color": "blue"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	val, err = s.GetSetting(ctx, "color")
	if err != nil {
		t.Fatalf("GetSetting after set: %v", err)
	}
	if val != "blue" {
		t.Errorf("expected 'blue', got %q", val)
	}

	// SaveSettings overwrites the existing value.
	if err := s.SaveSettings(ctx, map[string]string{"color": "red"}); err != nil {
		t.Fatalf("SaveSettings overwrite: %v", err)
	}
	val, _ = s.GetSetting(ctx, "color")
	if val != "red" {
		t.Errorf("expected 'red' after overwrite, got %q", val)
	}
}

// --- T8: SetVideoRating / ListVideosByRating ---

func TestSetVideoRating(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	if v.Rating != 0 {
		t.Errorf("expected default rating 0, got %d", v.Rating)
	}

	if err := s.SetVideoRating(ctx, v.ID, 1); err != nil {
		t.Fatalf("SetVideoRating 1: %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.Rating != 1 {
		t.Errorf("expected rating 1, got %d", got.Rating)
	}

	if err := s.SetVideoRating(ctx, v.ID, 2); err != nil {
		t.Fatalf("SetVideoRating 2: %v", err)
	}
	got, _ = s.GetVideo(ctx, v.ID)
	if got.Rating != 2 {
		t.Errorf("expected rating 2, got %d", got.Rating)
	}

	if err := s.SetVideoRating(ctx, v.ID, 0); err != nil {
		t.Fatalf("SetVideoRating reset: %v", err)
	}
	got, _ = s.GetVideo(ctx, v.ID)
	if got.Rating != 0 {
		t.Errorf("expected rating 0 after reset, got %d", got.Rating)
	}
}

func TestListVideosByRating(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "neutral.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "liked.mp4")
	v3, _ := s.UpsertVideo(ctx, d.ID, d.Path, "fav.mp4")

	s.SetVideoRating(ctx, v1.ID, 0) //nolint:errcheck
	s.SetVideoRating(ctx, v2.ID, 1) //nolint:errcheck
	s.SetVideoRating(ctx, v3.ID, 2) //nolint:errcheck

	videos, err := s.ListVideosByRating(ctx)
	if err != nil {
		t.Fatalf("ListVideosByRating: %v", err)
	}
	if len(videos) != 3 {
		t.Fatalf("expected 3 videos, got %d", len(videos))
	}
	// ORDER BY rating DESC: fav (2), liked (1), neutral (0)
	if videos[0].ID != v3.ID {
		t.Errorf("expected fav.mp4 first (rating 2), got %q", videos[0].Filename)
	}
	if videos[1].ID != v2.ID {
		t.Errorf("expected liked.mp4 second (rating 1), got %q", videos[1].Filename)
	}
	if videos[2].ID != v1.ID {
		t.Errorf("expected neutral.mp4 last (rating 0), got %q", videos[2].Filename)
	}
}

// --- T9: RecordWatch / GetWatch ---

func TestRecordAndGetWatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "show.mp4")

	// No watch record yet — should return an error.
	if _, err := s.GetWatch(ctx, v.ID); err == nil {
		t.Error("expected error for unwatched video, got nil")
	}

	if err := s.RecordWatch(ctx, v.ID, 123.45); err != nil {
		t.Fatalf("RecordWatch: %v", err)
	}

	rec, err := s.GetWatch(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetWatch: %v", err)
	}
	if rec.VideoID != v.ID {
		t.Errorf("expected VideoID %d, got %d", v.ID, rec.VideoID)
	}
	if rec.Position != 123.45 {
		t.Errorf("expected position 123.45, got %f", rec.Position)
	}
	if rec.WatchedAt == "" {
		t.Error("expected non-empty WatchedAt")
	}

	// RecordWatch is an upsert — re-recording updates the position.
	if err := s.RecordWatch(ctx, v.ID, 200.0); err != nil {
		t.Fatalf("RecordWatch update: %v", err)
	}
	rec2, _ := s.GetWatch(ctx, v.ID)
	if rec2.Position != 200.0 {
		t.Errorf("expected updated position 200.0, got %f", rec2.Position)
	}
}

func TestListWatchHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep1.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep2.mp4")

	// No watches yet — map should be empty.
	m, err := s.ListWatchHistory(ctx)
	if err != nil {
		t.Fatalf("ListWatchHistory (empty): %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty history, got %d entries", len(m))
	}

	s.RecordWatch(ctx, v1.ID, 30.0) //nolint:errcheck
	s.RecordWatch(ctx, v2.ID, 90.0) //nolint:errcheck

	m, err = s.ListWatchHistory(ctx)
	if err != nil {
		t.Fatalf("ListWatchHistory: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m[v1.ID].Position != 30.0 {
		t.Errorf("expected position 30.0 for v1, got %f", m[v1.ID].Position)
	}
	if m[v2.ID].WatchedAt == "" {
		t.Error("expected non-empty WatchedAt for v2")
	}
}

// --- GetRandomVideo ---

func TestGetRandomVideo_Empty(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetRandomVideo(context.Background())
	if err == nil {
		t.Fatal("expected error when no videos exist, got nil")
	}
}

func TestGetRandomVideo_ReturnsSomeVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, _ := s.AddDirectory(ctx, "/vids")
	s.UpsertVideo(ctx, d.ID, d.Path, "a.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "b.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "c.mp4")

	v, err := s.GetRandomVideo(ctx)
	if err != nil {
		t.Fatalf("GetRandomVideo: %v", err)
	}
	if v.Filename != "a.mp4" && v.Filename != "b.mp4" && v.Filename != "c.mp4" {
		t.Errorf("unexpected filename: %q", v.Filename)
	}
}

// --- SearchVideos ---

func TestSearchVideos_ByFilename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, _ := s.AddDirectory(ctx, "/vids")
	s.UpsertVideo(ctx, d.ID, d.Path, "nature_doc.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "cooking_show.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "nature_walk.mp4")

	results, err := s.SearchVideos(ctx, "nature")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestSearchVideos_ByDisplayName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, _ := s.AddDirectory(ctx, "/vids")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep01.mp4")
	s.UpdateVideoName(ctx, v.ID, "Planet Earth Episode 1")

	results, err := s.SearchVideos(ctx, "planet earth")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(results) != 1 || results[0].ID != v.ID {
		t.Errorf("expected 1 result for 'planet earth', got %+v", results)
	}
}

func TestSearchVideos_LIKESpecialChars(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, _ := s.AddDirectory(ctx, "/vids")
	s.UpsertVideo(ctx, d.ID, d.Path, "100%_real.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "other.mp4")

	// A bare "%" should match literally, not wildcard-everything.
	results, err := s.SearchVideos(ctx, "%")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(results) != 1 || results[0].Filename != "100%_real.mp4" {
		t.Errorf("expected only 100%%_real.mp4, got %+v", results)
	}
}

func TestSearchVideos_NoMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, _ := s.AddDirectory(ctx, "/vids")
	s.UpsertVideo(ctx, d.ID, d.Path, "something.mp4")

	results, err := s.SearchVideos(ctx, "zzznomatch")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- System tag tests ---

func TestSetExclusiveSystemTag_ReplacesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Set initial type tag.
	if err := s.SetExclusiveSystemTag(ctx, v.ID, "type", "Movie"); err != nil {
		t.Fatalf("SetExclusiveSystemTag(Movie): %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.VideoType != "Movie" {
		t.Errorf("expected VideoType=Movie, got %q", got.VideoType)
	}

	// Replace with a different type — old tag should be removed.
	if err := s.SetExclusiveSystemTag(ctx, v.ID, "type", "TV"); err != nil {
		t.Fatalf("SetExclusiveSystemTag(TV): %v", err)
	}
	got, _ = s.GetVideo(ctx, v.ID)
	if got.VideoType != "TV" {
		t.Errorf("expected VideoType=TV after replacement, got %q", got.VideoType)
	}

	// Verify only one type tag exists for this video.
	tags, _ := s.ListTagsByVideo(ctx, v.ID)
	typeTags := 0
	for _, tg := range tags {
		if len(tg.Name) > 5 && tg.Name[:5] == "type:" {
			typeTags++
		}
	}
	if typeTags != 1 {
		t.Errorf("expected exactly 1 type: tag, got %d", typeTags)
	}
}

func TestSetExclusiveSystemTag_EmptyRemoves(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	if err := s.SetExclusiveSystemTag(ctx, v.ID, "type", "Movie"); err != nil {
		t.Fatalf("SetExclusiveSystemTag(Movie): %v", err)
	}

	// Clear with empty value.
	if err := s.SetExclusiveSystemTag(ctx, v.ID, "type", ""); err != nil {
		t.Fatalf("SetExclusiveSystemTag(empty): %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.VideoType != "" {
		t.Errorf("expected VideoType empty after clear, got %q", got.VideoType)
	}
}

func TestSetMultiSystemTag_ActorSplit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	if err := s.SetMultiSystemTag(ctx, v.ID, "actor", []string{"Tom Hanks", "Meryl Streep"}); err != nil {
		t.Fatalf("SetMultiSystemTag: %v", err)
	}

	got, _ := s.GetVideo(ctx, v.ID)
	// Actors come back as GROUP_CONCAT, order may vary.
	if got.Actors == "" {
		t.Error("expected non-empty Actors field")
	}
	if !containsSubstr(got.Actors, "Tom Hanks") || !containsSubstr(got.Actors, "Meryl Streep") {
		t.Errorf("expected both actors in %q", got.Actors)
	}

	// Replace with a single actor — old actors should be removed.
	if err := s.SetMultiSystemTag(ctx, v.ID, "actor", []string{"Cate Blanchett"}); err != nil {
		t.Fatalf("SetMultiSystemTag replace: %v", err)
	}
	got, _ = s.GetVideo(ctx, v.ID)
	if got.Actors != "Cate Blanchett" {
		t.Errorf("expected Actors=Cate Blanchett after replace, got %q", got.Actors)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && (s[:len(sub)] == sub || s[len(s)-len(sub):] == sub || containsSubstrInner(s, sub)))
}

func containsSubstrInner(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestGetVideo_ShowNameFromTag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep01.mp4")

	if err := s.UpdateVideoShowName(ctx, v.ID, "Breaking Bad"); err != nil {
		t.Fatalf("UpdateVideoShowName: %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.ShowName != "Breaking Bad" {
		t.Errorf("expected ShowName=Breaking Bad, got %q", got.ShowName)
	}
}

func TestUpdateVideoFields_WritesSystemTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	f := store.VideoFields{
		Genre:         "Drama",
		SeasonNumber:  1,
		EpisodeNumber: 5,
		EpisodeTitle:  "The Beginning",
		Actors:        "Tom Hanks, Meryl Streep",
		Studio:        "Universal",
		Channel:       "HBO",
	}
	if err := s.UpdateVideoFields(ctx, v.ID, f); err != nil {
		t.Fatalf("UpdateVideoFields: %v", err)
	}

	got, _ := s.GetVideo(ctx, v.ID)
	if got.Genre != "Drama" {
		t.Errorf("Genre = %q, want Drama", got.Genre)
	}
	if got.Studio != "Universal" {
		t.Errorf("Studio = %q, want Universal", got.Studio)
	}
	if got.Channel != "HBO" {
		t.Errorf("Channel = %q, want HBO", got.Channel)
	}
	if got.SeasonNumber != 1 {
		t.Errorf("SeasonNumber = %d, want 1", got.SeasonNumber)
	}
	if got.EpisodeTitle != "The Beginning" {
		t.Errorf("EpisodeTitle = %q, want The Beginning", got.EpisodeTitle)
	}
	if !containsSubstr(got.Actors, "Tom Hanks") || !containsSubstr(got.Actors, "Meryl Streep") {
		t.Errorf("expected both actors in %q", got.Actors)
	}
}

func TestSearchVideos_MatchesTagName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep01.mp4")

	// Add a show system tag.
	if err := s.SetExclusiveSystemTag(ctx, v.ID, "show", "Breaking Bad"); err != nil {
		t.Fatalf("SetExclusiveSystemTag: %v", err)
	}

	// Search by tag value (partial match).
	results, err := s.SearchVideos(ctx, "Breaking")
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	found := false
	for _, r := range results {
		if r.ID == v.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected video with show:Breaking Bad to appear in search results for 'Breaking'")
	}
}

// --- duration_s and thumbnail_path column tests ---

func TestUpdateVideoDuration_PersistsAndIsReturnedByGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")

	if v.DurationS != 0 {
		t.Errorf("expected DurationS=0 on fresh upsert, got %f", v.DurationS)
	}

	if err := s.UpdateVideoDuration(ctx, v.ID, 3723.5); err != nil {
		t.Fatalf("UpdateVideoDuration: %v", err)
	}

	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DurationS != 3723.5 {
		t.Errorf("GetVideo DurationS = %f, want 3723.5", got.DurationS)
	}
}

func TestUpdateVideoDuration_IsReturnedByListVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")
	s.UpdateVideoDuration(ctx, v.ID, 120.0) //nolint:errcheck

	videos, err := s.ListVideos(ctx)
	if err != nil {
		t.Fatalf("ListVideos: %v", err)
	}
	if len(videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(videos))
	}
	if videos[0].DurationS != 120.0 {
		t.Errorf("ListVideos DurationS = %f, want 120.0", videos[0].DurationS)
	}
}

func TestUpdateVideoThumbnail_IsReturnedByListAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	// Fresh video should have no thumbnail.
	if v.ThumbnailPath != "" {
		t.Errorf("expected empty ThumbnailPath on upsert, got %q", v.ThumbnailPath)
	}

	thumbPath := "/videos/ep_thumb.jpg"
	if err := s.UpdateVideoThumbnail(ctx, v.ID, thumbPath); err != nil {
		t.Fatalf("UpdateVideoThumbnail: %v", err)
	}

	// GetVideo must return the path.
	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.ThumbnailPath != thumbPath {
		t.Errorf("GetVideo ThumbnailPath = %q, want %q", got.ThumbnailPath, thumbPath)
	}

	// ListVideos must also return the path (it was previously missing from the SELECT).
	videos, err := s.ListVideos(ctx)
	if err != nil {
		t.Fatalf("ListVideos: %v", err)
	}
	if len(videos) != 1 || videos[0].ThumbnailPath != thumbPath {
		t.Errorf("ListVideos ThumbnailPath = %q, want %q", videos[0].ThumbnailPath, thumbPath)
	}
}

func TestUpdateVideoDuration_ZeroIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "short.mp4")

	// Set a real duration, then overwrite with 0.
	s.UpdateVideoDuration(ctx, v.ID, 60.0) //nolint:errcheck
	s.UpdateVideoDuration(ctx, v.ID, 0.0)  //nolint:errcheck

	got, _ := s.GetVideo(ctx, v.ID)
	if got.DurationS != 0 {
		t.Errorf("expected DurationS=0 after reset, got %f", got.DurationS)
	}
}

// --- ListVideosByShow ---

func TestListVideosByShow_ReturnsOnlyMatchingShow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "bb_s01e01.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "bb_s01e02.mp4")
	v3, _ := s.UpsertVideo(ctx, d.ID, d.Path, "other_show.mp4")

	s.UpdateVideoShowName(ctx, v1.ID, "Breaking Bad") //nolint:errcheck
	s.UpdateVideoShowName(ctx, v2.ID, "Breaking Bad") //nolint:errcheck
	s.UpdateVideoShowName(ctx, v3.ID, "The Wire")     //nolint:errcheck

	vids, err := s.ListVideosByShow(ctx, "Breaking Bad")
	if err != nil {
		t.Fatalf("ListVideosByShow: %v", err)
	}
	if len(vids) != 2 {
		t.Fatalf("expected 2 videos for Breaking Bad, got %d", len(vids))
	}
	for _, v := range vids {
		if v.ShowName != "Breaking Bad" {
			t.Errorf("expected ShowName=Breaking Bad, got %q", v.ShowName)
		}
	}
}

func TestListVideosByShow_EmptyWhenNoMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")
	s.UpdateVideoShowName(ctx, v.ID, "Seinfeld") //nolint:errcheck

	vids, err := s.ListVideosByShow(ctx, "NoSuchShow")
	if err != nil {
		t.Fatalf("ListVideosByShow: %v", err)
	}
	if len(vids) != 0 {
		t.Errorf("expected 0 videos for unknown show, got %d", len(vids))
	}
}

// --- ListVideosByType ---

func TestListVideosByType_ReturnsOnlyMatchingType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "episode.mp4")
	v3, _ := s.UpsertVideo(ctx, d.ID, d.Path, "another_film.mp4")

	s.UpdateVideoType(ctx, v1.ID, "Movie") //nolint:errcheck
	s.UpdateVideoType(ctx, v2.ID, "TV")    //nolint:errcheck
	s.UpdateVideoType(ctx, v3.ID, "Movie") //nolint:errcheck

	movies, err := s.ListVideosByType(ctx, "Movie")
	if err != nil {
		t.Fatalf("ListVideosByType: %v", err)
	}
	if len(movies) != 2 {
		t.Fatalf("expected 2 Movie videos, got %d", len(movies))
	}
	for _, v := range movies {
		if v.VideoType != "Movie" {
			t.Errorf("expected VideoType=Movie, got %q", v.VideoType)
		}
	}
}

func TestListVideosByType_EmptyWhenNone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")
	s.UpdateVideoType(ctx, v.ID, "TV") //nolint:errcheck

	vids, err := s.ListVideosByType(ctx, "Concert")
	if err != nil {
		t.Fatalf("ListVideosByType: %v", err)
	}
	if len(vids) != 0 {
		t.Errorf("expected 0 Concert videos, got %d", len(vids))
	}
}

// --- UpdateVideoType ---

func TestUpdateVideoType_PersistsAndIsReturnedByGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	if err := s.UpdateVideoType(ctx, v.ID, "Movie"); err != nil {
		t.Fatalf("UpdateVideoType: %v", err)
	}
	got, _ := s.GetVideo(ctx, v.ID)
	if got.VideoType != "Movie" {
		t.Errorf("VideoType = %q, want Movie", got.VideoType)
	}

	// Can be cleared with empty string.
	if err := s.UpdateVideoType(ctx, v.ID, ""); err != nil {
		t.Fatalf("UpdateVideoType clear: %v", err)
	}
	got, _ = s.GetVideo(ctx, v.ID)
	if got.VideoType != "" {
		t.Errorf("VideoType = %q, want empty after clear", got.VideoType)
	}
}

// --- ListVideosByMinRating ---

func TestListVideosByMinRating_FiltersCorrectly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v0, _ := s.UpsertVideo(ctx, d.ID, d.Path, "neutral.mp4")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "liked.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "fav.mp4")

	s.SetVideoRating(ctx, v0.ID, 0) //nolint:errcheck
	s.SetVideoRating(ctx, v1.ID, 1) //nolint:errcheck
	s.SetVideoRating(ctx, v2.ID, 2) //nolint:errcheck

	// minRating=1 should return rated ≥1
	vids, err := s.ListVideosByMinRating(ctx, 1)
	if err != nil {
		t.Fatalf("ListVideosByMinRating(1): %v", err)
	}
	if len(vids) != 2 {
		t.Fatalf("expected 2 videos with rating ≥1, got %d", len(vids))
	}

	// minRating=2 should return only double-liked
	vids, err = s.ListVideosByMinRating(ctx, 2)
	if err != nil {
		t.Fatalf("ListVideosByMinRating(2): %v", err)
	}
	if len(vids) != 1 || vids[0].ID != v2.ID {
		t.Errorf("expected only fav.mp4 at minRating=2, got %+v", vids)
	}

	// minRating=0 returns everything
	vids, err = s.ListVideosByMinRating(ctx, 0)
	if err != nil {
		t.Fatalf("ListVideosByMinRating(0): %v", err)
	}
	if len(vids) != 3 {
		t.Errorf("expected 3 videos at minRating=0, got %d", len(vids))
	}
}

// --- ClearWatch ---

func TestClearWatch_RemovesRecord(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	s.RecordWatch(ctx, v.ID, 42.0) //nolint:errcheck

	if err := s.ClearWatch(ctx, v.ID); err != nil {
		t.Fatalf("ClearWatch: %v", err)
	}

	if _, err := s.GetWatch(ctx, v.ID); err == nil {
		t.Error("expected error getting watch after ClearWatch, got nil")
	}
}

func TestClearWatch_NoOpOnUnwatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")

	// ClearWatch on a never-watched video should not error.
	if err := s.ClearWatch(ctx, v.ID); err != nil {
		t.Errorf("ClearWatch on unwatched video: %v", err)
	}
}

// --- CountVideos ---

func TestCountVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.CountVideos(ctx)
	if err != nil {
		t.Fatalf("CountVideos (empty): %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}

	d, _ := s.AddDirectory(ctx, "/videos")
	s.UpsertVideo(ctx, d.ID, d.Path, "a.mp4") //nolint:errcheck
	s.UpsertVideo(ctx, d.ID, d.Path, "b.mp4") //nolint:errcheck

	n, err = s.CountVideos(ctx)
	if err != nil {
		t.Fatalf("CountVideos after inserts: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

// --- GetNextUnwatched ---

func TestGetNextUnwatched_ReturnsUnwatchedVideo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "aaa.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "bbb.mp4")

	// Watch v1; v2 should be returned as next unwatched.
	s.RecordWatch(ctx, v1.ID, 0) //nolint:errcheck

	next, err := s.GetNextUnwatched(ctx, 0)
	if err != nil {
		t.Fatalf("GetNextUnwatched: %v", err)
	}
	if next.ID != v2.ID {
		t.Errorf("expected bbb.mp4 (id=%d) as next unwatched, got %q (id=%d)", v2.ID, next.Filename, next.ID)
	}
}

func TestGetNextUnwatched_ErrorWhenAllWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "ep.mp4")
	s.RecordWatch(ctx, v.ID, 0) //nolint:errcheck

	if _, err := s.GetNextUnwatched(ctx, 0); err == nil {
		t.Error("expected error when all videos are watched, got nil")
	}
}

func TestGetNextUnwatched_WithTagFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "aaa.mp4")
	v2, _ := s.UpsertVideo(ctx, d.ID, d.Path, "bbb.mp4")

	tag, _ := s.UpsertTag(ctx, "playlist")
	// Only tag v1.
	s.TagVideo(ctx, v1.ID, tag.ID) //nolint:errcheck

	next, err := s.GetNextUnwatched(ctx, tag.ID)
	if err != nil {
		t.Fatalf("GetNextUnwatched with tagID: %v", err)
	}
	if next.ID != v1.ID {
		t.Errorf("expected aaa.mp4 as next unwatched in tag, got %q", next.Filename)
	}
	_ = v2
}

func TestListSettingsWithPrefix(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.SaveSettings(ctx, map[string]string{
		"folder_bg:Show A": "/img/a.jpg",
		"folder_bg:Show B": "/img/b.jpg",
		"other_key":        "ignore me",
	})

	// exact prefix
	got, err := s.ListSettingsWithPrefix(ctx, "folder_bg:")
	if err != nil {
		t.Fatalf("ListSettingsWithPrefix: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got["folder_bg:Show A"] != "/img/a.jpg" {
		t.Error("wrong value for Show A")
	}

	// no match
	none, _ := s.ListSettingsWithPrefix(ctx, "nonexistent:")
	if len(none) != 0 {
		t.Errorf("expected 0, got %d", len(none))
	}

	// empty prefix matches all (5 defaults + 3 we added)
	all, _ := s.ListSettingsWithPrefix(ctx, "")
	if len(all) != 8 {
		t.Errorf("expected 8, got %d", len(all))
	}
}

// --- Close ---

func TestClose(t *testing.T) {
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- DeleteDirectoryAndVideos ---

func TestDeleteDirectoryAndVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/movies")
	s.UpsertVideo(ctx, d.ID, d.Path, "a.mp4") //nolint:errcheck
	s.UpsertVideo(ctx, d.ID, d.Path, "b.mp4") //nolint:errcheck

	paths, err := s.DeleteDirectoryAndVideos(ctx, d.ID)
	if err != nil {
		t.Fatalf("DeleteDirectoryAndVideos: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 paths returned, got %d: %v", len(paths), paths)
	}

	// Directory and videos should be gone.
	dirs, _ := s.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories after delete, got %d", len(dirs))
	}
	videos, _ := s.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos after delete, got %d", len(videos))
	}
}

func TestDeleteDirectoryAndVideos_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/empty")

	paths, err := s.DeleteDirectoryAndVideos(ctx, d.ID)
	if err != nil {
		t.Fatalf("DeleteDirectoryAndVideos empty dir: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for empty directory, got %d", len(paths))
	}
}

// --- UpdateVideoPath ---

func TestUpdateVideoPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d1, _ := s.AddDirectory(ctx, "/old")
	d2, _ := s.AddDirectory(ctx, "/new")
	v, _ := s.UpsertVideo(ctx, d1.ID, d1.Path, "film.mp4")

	if err := s.UpdateVideoPath(ctx, v.ID, d2.ID, d2.Path, "renamed.mp4"); err != nil {
		t.Fatalf("UpdateVideoPath: %v", err)
	}

	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo after UpdateVideoPath: %v", err)
	}
	if got.DirectoryID != d2.ID {
		t.Errorf("expected DirectoryID=%d, got %d", d2.ID, got.DirectoryID)
	}
	if got.DirectoryPath != "/new" {
		t.Errorf("expected DirectoryPath=/new, got %q", got.DirectoryPath)
	}
	if got.Filename != "renamed.mp4" {
		t.Errorf("expected filename=renamed.mp4, got %q", got.Filename)
	}
}

// --- GetNextUnwatchedFromSearch ---

func TestGetNextUnwatchedFromSearch_EmptyQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "alpha.mp4")

	// Empty query delegates to GetNextUnwatched.
	got, err := s.GetNextUnwatchedFromSearch(ctx, "", 0)
	if err != nil {
		t.Fatalf("GetNextUnwatchedFromSearch empty query: %v", err)
	}
	if got.ID != v.ID {
		t.Errorf("expected video ID %d, got %d", v.ID, got.ID)
	}
}

func TestGetNextUnwatchedFromSearch_WithQuery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v1, _ := s.UpsertVideo(ctx, d.ID, d.Path, "nature_doc.mp4")
	s.UpsertVideo(ctx, d.ID, d.Path, "comedy.mp4") //nolint:errcheck

	got, err := s.GetNextUnwatchedFromSearch(ctx, "nature", 0)
	if err != nil {
		t.Fatalf("GetNextUnwatchedFromSearch: %v", err)
	}
	if got.ID != v1.ID {
		t.Errorf("expected nature_doc.mp4 (id %d), got id %d", v1.ID, got.ID)
	}
}

func TestGetNextUnwatchedFromSearch_AllWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	d, _ := s.AddDirectory(ctx, "/videos")
	v, _ := s.UpsertVideo(ctx, d.ID, d.Path, "doc.mp4")
	s.RecordWatch(ctx, v.ID, 1.0) //nolint:errcheck

	_, err := s.GetNextUnwatchedFromSearch(ctx, "doc", 0)
	if err == nil {
		t.Error("expected error when all matched videos are watched, got nil")
	}
}

// --- Sessions ---

func TestSaveAndLoadSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	expiry := time.Now().Add(time.Hour)
	if err := s.SaveSession(ctx, "tok-abc", expiry); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	sessions, err := s.LoadSessions(ctx)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if _, ok := sessions["tok-abc"]; !ok {
		t.Errorf("expected token tok-abc in sessions, got %v", sessions)
	}
}

func TestSaveSession_Upsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	exp1 := time.Now().Add(time.Hour)
	exp2 := time.Now().Add(2 * time.Hour)

	s.SaveSession(ctx, "tok-dup", exp1) //nolint:errcheck
	if err := s.SaveSession(ctx, "tok-dup", exp2); err != nil {
		t.Fatalf("SaveSession upsert: %v", err)
	}

	sessions, _ := s.LoadSessions(ctx)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session after upsert, got %d", len(sessions))
	}
}

func TestDeleteSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SaveSession(ctx, "tok-del", time.Now().Add(time.Hour)) //nolint:errcheck

	if err := s.DeleteSession(ctx, "tok-del"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	sessions, _ := s.LoadSessions(ctx)
	if _, ok := sessions["tok-del"]; ok {
		t.Error("expected token to be deleted")
	}
}

func TestPruneExpiredSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Save one expired (past) and one valid (future) session.
	s.SaveSession(ctx, "tok-valid", time.Now().Add(time.Hour))    //nolint:errcheck
	s.SaveSession(ctx, "tok-expired", time.Now().Add(-time.Hour)) //nolint:errcheck

	if err := s.PruneExpiredSessions(ctx); err != nil {
		t.Fatalf("PruneExpiredSessions: %v", err)
	}

	sessions, _ := s.LoadSessions(ctx)
	if _, ok := sessions["tok-valid"]; !ok {
		t.Error("expected valid session to survive pruning")
	}
	if _, ok := sessions["tok-expired"]; ok {
		t.Error("expected expired session to be pruned")
	}
}

func TestLoadSessions_ExcludesExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SaveSession(ctx, "alive", time.Now().Add(time.Hour))   //nolint:errcheck
	s.SaveSession(ctx, "dead", time.Now().Add(-time.Minute)) //nolint:errcheck

	sessions, err := s.LoadSessions(ctx)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if _, ok := sessions["alive"]; !ok {
		t.Error("expected 'alive' session in result")
	}
	if _, ok := sessions["dead"]; ok {
		t.Error("expired 'dead' session should not appear in LoadSessions result")
	}
}
