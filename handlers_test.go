package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

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
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "alpha.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "beta.mkv")
	// assign a show name to one video to verify grouping header
	srv.store.UpdateVideoShowName(ctx, v1.ID, "MyShow")

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
	if !strings.Contains(body, "MyShow") {
		t.Error("expected show header MyShow in response")
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

func TestListVideosByType(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "a.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "b.mp4")
	srv.store.UpdateVideoType(ctx, v1.ID, "TV")
	srv.store.UpdateVideoType(ctx, v2.ID, "Movie")

	list, err := srv.store.ListVideosByType(ctx, "TV")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != v1.ID {
		t.Errorf("expected only v1, got %v", list)
	}
}

func TestHandleSetVideoType(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "x.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/type", strings.NewReader("type=TV"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	updated, _ := srv.store.GetVideo(ctx, v.ID)
	if updated.VideoType != "TV" {
		t.Errorf("expected type TV, got %q", updated.VideoType)
	}

	// invalid type should 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/type", strings.NewReader("type=foo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", rec.Code)
	}
}

func TestHandleVideoList_FilterByType(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "t1.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "t2.mp4")
	srv.store.UpdateVideoType(ctx, v1.ID, "TV")
	srv.store.UpdateVideoType(ctx, v2.ID, "Movie")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos?type=TV", nil)
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "t1.mp4") || strings.Contains(body, "t2.mp4") {
		t.Errorf("unexpected body %s", body)
	}
}

func TestHandleVideoList_FilterCombination(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "combo1.mp4")
	srv.store.UpdateVideoType(ctx, v1.ID, "TV")
	srv.store.SetVideoRating(ctx, v1.ID, 1)
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "combo2.mp4")
	srv.store.UpdateVideoType(ctx, v2.ID, "TV")
	srv.store.SetVideoRating(ctx, v2.ID, 0)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos?type=TV&rating=1", nil)
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "combo1.mp4") || strings.Contains(body, "combo2.mp4") {
		t.Errorf("unexpected body %s", body)
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

func TestServeVideoListPagination(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	// Seed 5 videos with unique names
	names := []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4", "e.mp4"}
	for _, n := range names {
		srv.store.UpsertVideo(ctx, d.ID, d.Path, n) //nolint:errcheck
	}

	// Page 1 with limit=2 should return 2 videos
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos?page=1&limit=2", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d", rec.Code)
	}

	// Page 3 with limit=2 should return 1 video (only "e.mp4")
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/videos?page=3&limit=2", nil)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("page3: expected 200, got %d", rec2.Code)
	}

	// Page 10 (out-of-range) should return 200 with no video rows
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/videos?page=10&limit=2", nil)
	srv.routes().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("page10: expected 200, got %d", rec3.Code)
	}
}

func TestHandleQuickLabelModal_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/quick-label", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<form`) {
		t.Error("expected a form element in the quick-label modal")
	}
}

func TestHandleQuickLabelModal_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/999/quick-label", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleQuickLabelModal_WithTagsAndDirs(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, t.TempDir())
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	tag, _ := srv.store.UpsertTag(ctx, "scifi")
	_ = srv.store.TagVideo(ctx, v.ID, tag.ID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/quick-label", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "scifi") {
		t.Error("expected tag 'scifi' in modal body")
	}
	if !strings.Contains(body, "ql-move-dir-") {
		t.Error("expected move-to-dir select in modal body")
	}
}

func TestHandleQuickLabelModal(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/quick-label", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleQuickLabelSubmit_OK(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{
		"name":          {"My Film"},
		"genre":         {"Drama"},
		"season":        {"2"},
		"episode":       {"5"},
		"episode_title": {"The Beginning"},
		"actors":        {"Tom Hanks"},
		"studio":        {"WB"},
		"channel":       {"HBO"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/quick-label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Genre != "Drama" {
		t.Errorf("Genre = %q, want Drama", got.Genre)
	}
	if got.SeasonNumber != 2 {
		t.Errorf("SeasonNumber = %d, want 2", got.SeasonNumber)
	}
	if got.Actors != "Tom Hanks" {
		t.Errorf("Actors = %q, want Tom Hanks", got.Actors)
	}
	// Name update is applied when non-empty.
	if got.Title() != "My Film" {
		t.Errorf("Title = %q, want My Film", got.Title())
	}
}

func TestHandleQuickLabelSubmit_NotFound(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"genre": {"Action"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/quick-label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleQuickLabelSubmit_AirDate(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{
		"air_date": {"2023-04-15"},
		"genre":    {"Drama"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/quick-label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, err := srv.store.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.AirDate != "2023-04-15" {
		t.Errorf("AirDate = %q, want 2023-04-15", got.AirDate)
	}
}

func TestHandleQuickLabelSubmit(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{
		"name":          {"My Film"},
		"genre":         {"Drama"},
		"season":        {"2"},
		"episode":       {"5"},
		"episode_title": {"Pilot"},
		"actors":        {"Jane Doe"},
		"studio":        {"HBO"},
		"channel":       {"cable"},
		"air_date":      {"2024-01-15"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/quick-label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Title() != "My Film" {
		t.Errorf("expected title 'My Film', got %q", got.Title())
	}
}

func TestAddVideoTag_RejectsReservedPrefix(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	reservedTags := []string{"show:Breaking Bad", "type:TV", "genre:Drama", "actor:Tom Hanks", "studio:HBO", "channel:AMC"}
	for _, tagName := range reservedTags {
		form := url.Values{"tag": {tagName}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/tags", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)

		// Should return 200 with an error message (not actually add the tag).
		if rec.Code != http.StatusOK {
			t.Errorf("tag %q: expected 200 with error HTML, got %d", tagName, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Use the dedicated field") {
			t.Errorf("tag %q: expected rejection message, got: %s", tagName, body)
		}

		// Ensure the tag was not actually added.
		tags, _ := srv.store.ListTagsByVideo(ctx, v.ID)
		for _, tg := range tags {
			if tg.Name == tagName {
				t.Errorf("tag %q should have been rejected but was added", tagName)
			}
		}
	}
}

func TestSetExclusiveSystemTag_ShowAndType(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep01.mp4")

	// Set show name via store method.
	if err := srv.store.UpdateVideoShowName(ctx, v.ID, "The Wire"); err != nil {
		t.Fatalf("UpdateVideoShowName: %v", err)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.ShowName != "The Wire" {
		t.Errorf("ShowName = %q, want 'The Wire'", got.ShowName)
	}

	// Update type via store method.
	if err := srv.store.UpdateVideoType(ctx, v.ID, "TV"); err != nil {
		t.Fatalf("UpdateVideoType: %v", err)
	}
	got, _ = srv.store.GetVideo(ctx, v.ID)
	if got.VideoType != "TV" {
		t.Errorf("VideoType = %q, want 'TV'", got.VideoType)
	}

	// ListVideosByShow should find the video.
	videos, err := srv.store.ListVideosByShow(ctx, "The Wire")
	if err != nil {
		t.Fatalf("ListVideosByShow: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != v.ID {
		t.Errorf("ListVideosByShow: expected video %d, got %+v", v.ID, videos)
	}

	// ListVideosByType should find the video.
	videos, err = srv.store.ListVideosByType(ctx, "TV")
	if err != nil {
		t.Fatalf("ListVideosByType: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != v.ID {
		t.Errorf("ListVideosByType: expected video %d, got %+v", v.ID, videos)
	}
}
