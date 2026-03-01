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

// --- T10: GetSetting / SetSetting ---

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

	// SetSetting creates the row.
	if err := s.SetSetting(ctx, "color", "blue"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	val, err = s.GetSetting(ctx, "color")
	if err != nil {
		t.Fatalf("GetSetting after set: %v", err)
	}
	if val != "blue" {
		t.Errorf("expected 'blue', got %q", val)
	}

	// SetSetting overwrites the existing value.
	if err := s.SetSetting(ctx, "color", "red"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
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
