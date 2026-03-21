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
	"time"

	"github.com/maxgarvey/video_manger/store"
)

func TestHandleAPIListVideos_ReturnsAll(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiVideo
	code := apiGet(t, srv, "/api/videos", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 4 {
		t.Errorf("expected 4 videos, got %d", len(result))
	}
	// Every entry must have a stream_url.
	for _, v := range result {
		if v.StreamURL == "" {
			t.Errorf("video %d has empty stream_url", v.ID)
		}
	}
}

func TestHandleAPIListVideos_FilterByType(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiVideo
	code := apiGet(t, srv, "/api/videos?type=Movie", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 Movie, got %d: %+v", len(result), result)
	}
	if result[0].Type != "Movie" {
		t.Errorf("type = %q, want Movie", result[0].Type)
	}
}

func TestHandleAPIListVideos_DurationSPopulated(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiVideo
	apiGet(t, srv, "/api/videos", &result)

	byFile := make(map[string]apiVideo)
	for _, v := range result {
		byFile[v.Title] = v
	}
	if byFile["movie.mp4"].DurationS != 5400 {
		t.Errorf("movie duration_s = %f, want 5400", byFile["movie.mp4"].DurationS)
	}
	if byFile["s01e01.mp4"].DurationS != 600 {
		t.Errorf("s01e01 duration_s = %f, want 600", byFile["s01e01.mp4"].DurationS)
	}
	// Video with no duration should serialize as omitted (zero).
	if byFile["s02e01.mp4"].DurationS != 0 {
		t.Errorf("s02e01 duration_s = %f, want 0", byFile["s02e01.mp4"].DurationS)
	}
}

func TestHandleAPIGetVideo_ValidID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "clip.mp4")

	var result apiVideo
	code := apiGet(t, srv, "/api/videos/"+itoa(v.ID), &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if result.ID != v.ID {
		t.Errorf("id = %d, want %d", result.ID, v.ID)
	}
	if result.StreamURL != "/video/"+itoa(v.ID) {
		t.Errorf("stream_url = %q, want /video/%s", result.StreamURL, itoa(v.ID))
	}
}

func TestHandleAPIGetVideo_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/videos/99999", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleAPIGetVideo_InvalidID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/videos/notanid", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIRandom_ReturnsVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "only.mp4") //nolint:errcheck

	var result apiVideo
	code := apiGet(t, srv, "/api/random", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if result.StreamURL == "" {
		t.Error("random video has empty stream_url")
	}
}

func TestHandleAPIRandom_EmptyLibraryIs404(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 on empty library, got %d", rec.Code)
	}
}

func TestHandleAPIListShows_AggregatesSeasonAndEpisodeCounts(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiShow
	code := apiGet(t, srv, "/api/shows", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	// seedAPIFixture has one show ("Alpha") with 2 seasons and 3 episodes.
	if len(result) != 1 {
		t.Fatalf("expected 1 show, got %d: %+v", len(result), result)
	}
	show := result[0]
	if show.Title != "Alpha" {
		t.Errorf("show title = %q, want Alpha", show.Title)
	}
	if show.SeasonCount != 2 {
		t.Errorf("season_count = %d, want 2", show.SeasonCount)
	}
	if show.EpisodeCount != 3 {
		t.Errorf("episode_count = %d, want 3", show.EpisodeCount)
	}
}

func TestHandleAPIListShows_ExcludesVideosWithNoShow(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv) // includes movie.mp4 with no show name

	var result []apiShow
	apiGet(t, srv, "/api/shows", &result)
	for _, s := range result {
		if s.Title == "" {
			t.Error("shows list contains an entry with empty title (no-show video leaked in)")
		}
	}
}

func TestHandleAPIListSeasons_CountsEpisodesPerSeason(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiSeason
	code := apiGet(t, srv, "/api/shows/Alpha/seasons", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 seasons, got %d: %+v", len(result), result)
	}
	// Seasons must be sorted ascending.
	if result[0].Number != 1 || result[1].Number != 2 {
		t.Errorf("season numbers = %d, %d; want 1, 2", result[0].Number, result[1].Number)
	}
	if result[0].EpisodeCount != 2 {
		t.Errorf("season 1 episode_count = %d, want 2", result[0].EpisodeCount)
	}
	if result[1].EpisodeCount != 1 {
		t.Errorf("season 2 episode_count = %d, want 1", result[1].EpisodeCount)
	}
}

func TestHandleAPIListEpisodes_SortedByEpisodeNumber(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiVideo
	code := apiGet(t, srv, "/api/shows/Alpha/seasons/1/episodes", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 episodes in season 1, got %d", len(result))
	}
	if result[0].Episode != 1 || result[1].Episode != 2 {
		t.Errorf("episodes not sorted: got %d, %d", result[0].Episode, result[1].Episode)
	}
}

func TestHandleAPIListEpisodes_WrongSeasonReturnsEmpty(t *testing.T) {
	srv := newTestServer(t)
	seedAPIFixture(t, srv)

	var result []apiVideo
	code := apiGet(t, srv, "/api/shows/Alpha/seasons/99/episodes", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 episodes for season 99, got %d", len(result))
	}
}

func TestHandleAPIListTags_ReturnsAllTags(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.UpsertTag(ctx, "comedy")  //nolint:errcheck
	srv.store.UpsertTag(ctx, "classic") //nolint:errcheck

	var result []apiTag
	code := apiGet(t, srv, "/api/tags", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	names := make(map[string]bool)
	for _, tg := range result {
		names[tg.Name] = true
	}
	for _, want := range []string{"comedy", "classic"} {
		if !names[want] {
			t.Errorf("tag %q missing from /api/tags response", want)
		}
	}
}

func TestHandleAPITagVideos_ReturnsVideosWithTag(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "a.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "b.mp4")
	tag, _ := srv.store.UpsertTag(ctx, "fav")
	srv.store.TagVideo(ctx, v1.ID, tag.ID) //nolint:errcheck
	srv.store.TagVideo(ctx, v2.ID, tag.ID) //nolint:errcheck

	var result []apiVideo
	code := apiGet(t, srv, "/api/tags/"+itoa(tag.ID)+"/videos", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 videos for tag, got %d", len(result))
	}
}

func TestHandleAPIRecentlyWatched_SortedMostRecentFirst(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "old.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "new.mp4")
	// Record v1 first, then sleep 1s so v2 gets a strictly later watched_at
	// timestamp. SQLite's datetime('now') has second precision, so without the
	// sleep both records share the same timestamp and the sort order is
	// undefined.
	srv.store.RecordWatch(ctx, v1.ID, 60.0) //nolint:errcheck
	time.Sleep(time.Second)
	srv.store.RecordWatch(ctx, v2.ID, 120.0) //nolint:errcheck

	var result []apiWatchedEntry
	code := apiGet(t, srv, "/api/recently-watched", &result)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(result) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(result))
	}
	// Most-recent first: v2 should come before v1.
	if result[0].ID != v2.ID {
		t.Errorf("first entry ID = %d, want %d (most recently watched)", result[0].ID, v2.ID)
	}
}

func TestHandleAPIRecentlyWatched_IncludesPosition(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	srv.store.RecordWatch(ctx, v.ID, 937.5) //nolint:errcheck

	var result []apiWatchedEntry
	apiGet(t, srv, "/api/recently-watched", &result)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].PositionS != 937.5 {
		t.Errorf("position_s = %f, want 937.5", result[0].PositionS)
	}
}

func TestHandleAPIListVideos_ThumbnailURLPresent(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "thumb.mp4")
	srv.store.UpdateVideoThumbnail(ctx, v.ID, "/lib/thumb_thumb.jpg") //nolint:errcheck

	var result []apiVideo
	apiGet(t, srv, "/api/videos", &result)
	if len(result) != 1 {
		t.Fatalf("expected 1 video, got %d", len(result))
	}
	want := "/videos/" + itoa(v.ID) + "/thumbnail"
	if result[0].ThumbnailURL != want {
		t.Errorf("thumbnail_url = %q, want %q", result[0].ThumbnailURL, want)
	}
}

func TestHandleAPIListVideos_NoThumbnailURLWhenAbsent(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "nothumb.mp4") //nolint:errcheck

	var result []apiVideo
	apiGet(t, srv, "/api/videos", &result)
	if len(result) != 1 {
		t.Fatalf("expected 1 video, got %d", len(result))
	}
	if result[0].ThumbnailURL != "" {
		t.Errorf("expected empty thumbnail_url, got %q", result[0].ThumbnailURL)
	}
}

func TestHandleAPIDirectories_Empty(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/directories", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var dirs []struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&dirs); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected empty list, got %d entries", len(dirs))
	}
}

func TestHandleAPIDirectories_WithDirs(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.AddDirectory(ctx, "/videos/movies") //nolint:errcheck
	srv.store.AddDirectory(ctx, "/videos/tv")     //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/directories", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var dirs []struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&dirs); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(dirs) != 2 {
		t.Errorf("expected 2 directories, got %d", len(dirs))
	}
	paths := make(map[string]bool)
	for _, d := range dirs {
		paths[d.Path] = true
		if d.ID == 0 {
			t.Errorf("directory %q has zero ID", d.Path)
		}
	}
	if !paths["/videos/movies"] || !paths["/videos/tv"] {
		t.Errorf("missing expected paths, got %v", paths)
	}
}

func TestHandleGetFolderBackgrounds_Empty(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/folder-backgrounds", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestHandleGetFolderBackgrounds_WithData(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	_ = srv.store.SaveSettings(ctx, map[string]string{
		"folder_bg:Show A": "/img/a.jpg",
		"folder_bg:Show B": "/img/b.jpg",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/folder-backgrounds", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result["Show A"] != "/img/a.jpg" {
		t.Errorf("wrong path for Show A: %q", result["Show A"])
	}
	if result["Show B"] != "/img/b.jpg" {
		t.Errorf("wrong path for Show B: %q", result["Show B"])
	}
}

func TestHandleSetFolderBackground(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// valid POST
	form := url.Values{"show": {"Breaking Bad"}, "path": {"/img/bb.jpg"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/folder-background", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	val, _ := srv.store.GetSetting(ctx, "folder_bg:Breaking Bad")
	if val != "/img/bb.jpg" {
		t.Errorf("setting = %q, want /img/bb.jpg", val)
	}

	// missing show → 400
	form = url.Values{"path": {"/img/x.jpg"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/folder-background", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleServeImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(imgPath, []byte("FAKEIMG"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	_, _ = srv.store.AddDirectory(ctx, dir)

	// valid path → 200 with content
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/serve-image?path="+url.QueryEscape(imgPath), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid path: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FAKEIMG") {
		t.Error("expected file content in body")
	}

	// path outside any registered dir → 403
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/serve-image?path="+url.QueryEscape("/etc/passwd"), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign path: expected 403, got %d", rec.Code)
	}

	// missing path param → 400
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/serve-image", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no path: expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIListVideos_FilterByShow(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show_ep1.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4") //nolint:errcheck
	srv.store.UpdateVideoShowName(ctx, v1.ID, "MyShow")   //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/videos?show=MyShow", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "show_ep1.mp4") {
		t.Error("expected show_ep1.mp4 in response")
	}
	if strings.Contains(rec.Body.String(), "movie.mp4") {
		t.Error("movie.mp4 should not appear when filtering by show")
	}
}

func TestHandleAPIListVideos_FilterByTypeAPI(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "tv_ep.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "movie.mp4")
	srv.store.UpdateVideoType(ctx, v1.ID, "TV")    //nolint:errcheck
	srv.store.UpdateVideoType(ctx, v2.ID, "Movie") //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/videos?type=TV", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tv_ep.mp4") {
		t.Error("expected tv_ep.mp4 in type=TV response")
	}
	if strings.Contains(body, "movie.mp4") {
		t.Error("movie.mp4 should not appear when filtering by type=TV")
	}
}

func TestHandleAPIListEpisodes(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v1, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show_s1e1.mp4")
	v2, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show_s2e1.mp4")
	srv.store.UpdateVideoShowName(ctx, v1.ID, "TestShow") //nolint:errcheck
	srv.store.UpdateVideoShowName(ctx, v2.ID, "TestShow") //nolint:errcheck
	srv.store.UpdateVideoFields(ctx, v1.ID, store.VideoFields{SeasonNumber: 1, EpisodeNumber: 1}) //nolint:errcheck
	srv.store.UpdateVideoFields(ctx, v2.ID, store.VideoFields{SeasonNumber: 2, EpisodeNumber: 1}) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/shows/TestShow/seasons/1/episodes", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "show_s1e1.mp4") {
		t.Error("expected season 1 episode in response")
	}
	if strings.Contains(body, "show_s2e1.mp4") {
		t.Error("season 2 episode should not appear in season 1 response")
	}
}

func TestHandleAPITagVideos(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "tagged.mp4")
	tag, _ := srv.store.UpsertTag(ctx, "action")
	srv.store.TagVideo(ctx, v.ID, tag.ID) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/tags/%d/videos", tag.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "tagged.mp4") {
		t.Error("expected tagged.mp4 in tag-filtered response")
	}
}

func TestHandleAPIListTags(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.UpsertTag(ctx, "comedy") //nolint:errcheck
	srv.store.UpsertTag(ctx, "drama")  //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "comedy") || !strings.Contains(body, "drama") {
		t.Error("expected both tags in API response")
	}
}

func TestHandleAPITagVideos_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tags/notanid/videos", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad tag ID, got %d", rec.Code)
	}
}

func TestHandleAPIListVideos_FilterByTagID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "tagged.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "other.mp4") //nolint:errcheck
	tag, _ := srv.store.UpsertTag(ctx, "scifi")
	srv.store.TagVideo(ctx, v.ID, tag.ID) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/videos?tag_id=%d", tag.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tagged.mp4") {
		t.Error("expected tagged.mp4 in response")
	}
	if strings.Contains(body, "other.mp4") {
		t.Error("other.mp4 should not appear in tag-filtered response")
	}
}

func TestHandleAPIListVideos_SearchQuery(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "nature_doc.mp4") //nolint:errcheck
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "comedy.mp4")     //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/videos?q=nature", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "nature_doc.mp4") {
		t.Error("expected nature_doc.mp4 in search results")
	}
	if strings.Contains(body, "comedy.mp4") {
		t.Error("comedy.mp4 should not appear in search results")
	}
}

func TestHandleAPIListEpisodes_InvalidSeason(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/shows/Alpha/seasons/notanumber/episodes", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for invalid season, got %d", rec.Code)
	}
}
