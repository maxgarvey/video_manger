package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

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
		"title":         {"My Show"},
		"description":   {"A great show"},
		"genre":         {"Drama"},
		"date":          {"2024-01-01"},
		"show":          {"My Show"},
		"network":       {"HBO"},
		"episode_id":    {"S01E01"},
		"season_number": {"1"},
		"episode_sort":  {"1"},
		"comment":       {""},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleGetLookupModal_NoKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Settings") {
		t.Error("expected 'Settings' directive in response when no API key configured")
	}
}

func TestHandleGetLookupModal_WithKey(t *testing.T) {
	// T2: modal with API key set should render the search form.
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Search TMDB") {
		t.Error("expected search form in response when API key is configured")
	}
}

func TestHandleLookupSearch_NoKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {"batman"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no API key, got %d", rec.Code)
	}
}

func TestHandleLookupSearch_BadRequest(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", rec.Code)
	}
}

func TestHandleLookupSearch_Success(t *testing.T) {
	// Spin up a mock TMDB server.
	mockTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"id":550,"media_type":"movie","title":"Fight Club","release_date":"1999-10-15"}]}`)) //nolint:errcheck
	}))
	defer mockTMDB.Close()

	// Replace the package-level tmdbClient transport so requests go to the mock.
	orig := tmdbClient
	tmdbClient = &http.Client{Transport: &tmdbRoundTripper{host: strings.TrimPrefix(mockTMDB.URL, "http://")}}
	defer func() { tmdbClient = orig }()

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-test-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"q": {"Fight Club"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from TMDB search, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Fight Club") {
		t.Error("expected TMDB result 'Fight Club' in response")
	}
}

func TestHandleLookupApply_BadMediaType(t *testing.T) {
	// T3: invalid media_type should return 400.
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"media_type": {"book"}, "tmdb_id": {"123"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media_type, got %d", rec.Code)
	}
}

func TestHandleLookupApply_BadVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck

	form := url.Values{"media_type": {"movie"}, "tmdb_id": {"12345"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/999/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown video, got %d", rec.Code)
	}
}

func TestHandleLookupApply_Movie(t *testing.T) {
	cleanup := withMockTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"title":"Fight Club","overview":"A soap salesman.","release_date":"1999-10-15","genres":[{"name":"Drama"}]}`)) //nolint:errcheck
	})
	defer cleanup()

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"media_type": {"movie"}, "tmdb_id": {"550"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from movie apply, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLookupApply_TV(t *testing.T) {
	cleanup := withMockTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		if strings.Contains(path, "/season/") && strings.Contains(path, "/episode/") {
			w.Write([]byte(`{"name":"Pilot","overview":"First episode.","air_date":"2008-01-20"}`)) //nolint:errcheck
		} else if strings.Contains(path, "/tv/") {
			w.Write([]byte(`{"name":"Breaking Bad","networks":[{"name":"AMC"}],"genres":[{"name":"Drama"}]}`)) //nolint:errcheck
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer cleanup()

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "s01e01.mp4")

	form := url.Values{"media_type": {"tv"}, "tmdb_id": {"1396"}, "season": {"1"}, "episode": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from TV apply, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLookupApply_NoApiKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"media_type": {"movie"}, "tmdb_id": {"123"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/lookup/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing API key, got %d", rec.Code)
	}
}

func TestHandleLookupEpisodes_Success(t *testing.T) {
	cleanup := withMockTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"episodes":[{"id":1,"name":"Pilot","episode_number":1},{"id":2,"name":"Cat's in the Bag","episode_number":2}]}`)) //nolint:errcheck
	})
	defer cleanup()

	srv := newTestServer(t)
	ctx := context.Background()
	srv.store.SaveSettings(ctx, map[string]string{"tmdb_api_key": "fake-key"}) //nolint:errcheck
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "s01e01.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup/episodes?tmdb_id=1396&season=1", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from episode list, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleLookupEpisodes_NoApiKey(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "show.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/lookup/episodes?tmdb_id=123&season=1", nil)
	srv.routes().ServeHTTP(rec, req)

	// Should return 200 with an HTML error message (not a redirect or 400).
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TMDB API key") {
		t.Error("expected TMDB API key message in response")
	}
}

func TestHandleLookupEpisodes_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/notanid/lookup/episodes", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 404 or 400, got %d", rec.Code)
	}
}

func TestHandleGetVideoFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/fields", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// view template must contain the stable div id
	if !strings.Contains(rec.Body.String(), "video-fields-"+itoa(v.ID)) {
		t.Error("expected video-fields div id in response")
	}
}

func TestHandleGetVideoFields_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/9999/fields", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleEditVideoFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/videos/"+itoa(v.ID)+"/fields/edit", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Edit form must contain inputs for genre and actors
	if !strings.Contains(body, `name="genre"`) {
		t.Error("expected genre input in edit form")
	}
	if !strings.Contains(body, `name="actors"`) {
		t.Error("expected actors input in edit form")
	}
}

func TestHandleUpdateVideoFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{
		"genre":          {"Action"},
		"channel":        {"HBO"},
		"season_number":  {"2"},
		"episode_number": {"5"},
		"episode_title":  {"Pilot"},
		"actors":         {"Tom Hanks"},
		"studio":         {"WB"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/fields", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Response is the view template; should contain saved values
	body := rec.Body.String()
	if !strings.Contains(body, "Action") {
		t.Error("expected genre in response")
	}
	if !strings.Contains(body, "Tom Hanks") {
		t.Error("expected actors in response")
	}
	// Verify DB was updated
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Genre != "Action" {
		t.Errorf("Genre = %q, want Action", got.Genre)
	}
	if got.SeasonNumber != 2 {
		t.Errorf("SeasonNumber = %d, want 2", got.SeasonNumber)
	}
}

func TestHandleUpdateVideoFields_ZeroValues(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Submit all-empty fields — should succeed and persist zeros
	form := url.Values{
		"genre": {""}, "channel": {""}, "season_number": {"0"},
		"episode_number": {"0"}, "episode_title": {""}, "actors": {""}, "studio": {""},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/videos/"+itoa(v.ID)+"/fields", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got, _ := srv.store.GetVideo(ctx, v.ID)
	if got.Genre != "" || got.SeasonNumber != 0 {
		t.Errorf("expected empty fields, got Genre=%q Season=%d", got.Genre, got.SeasonNumber)
	}
}

func TestHandleSettings(t *testing.T) {
	srv := newTestServer(t)

	// GET — returns settings form.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "autoplay_random") {
		t.Error("expected autoplay_random in settings form")
	}

	// POST — save settings.
	form := url.Values{
		"autoplay_random": {"on"},
		"video_sort":      {"rating"},
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST settings: expected 200, got %d", rec.Code)
	}

	ctx := context.Background()
	val, _ := srv.store.GetSetting(ctx, "video_sort")
	if val != "rating" {
		t.Errorf("expected video_sort=rating, got %q", val)
	}
	val, _ = srv.store.GetSetting(ctx, "autoplay_random")
	if val != "true" {
		t.Errorf("expected autoplay_random=true, got %q", val)
	}
}

func TestHandleSaveSettings_AutoplayOn(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{
		"autoplay_random":  {"on"},
		"next_from_search": {"on"},
		"video_sort":       {"name"},
		"library_path":     {"/tmp/lib"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ctx := context.Background()
	autoplay, _ := srv.store.GetSetting(ctx, "autoplay_random")
	if autoplay != "true" {
		t.Errorf("expected autoplay_random=true, got %q", autoplay)
	}
	nextSearch, _ := srv.store.GetSetting(ctx, "next_from_search")
	if nextSearch != "true" {
		t.Errorf("expected next_from_search=true, got %q", nextSearch)
	}
}

func TestHandleSaveSettings_WithTmdbKey(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"tmdb_api_key": {"mykey123"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ctx := context.Background()
	key, _ := srv.store.GetSetting(ctx, "tmdb_api_key")
	if key != "mykey123" {
		t.Errorf("expected tmdb_api_key=mykey123, got %q", key)
	}
}

func TestHandleListTags_Empty(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleListTags_WithTags(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")
	tag, _ := srv.store.UpsertTag(ctx, "sci-fi")
	srv.store.TagVideo(ctx, v.ID, tag.ID) //nolint:errcheck

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sci-fi") {
		t.Error("expected tag name in response")
	}
}

func TestParseFilenameHints_ExtractsSeasonEpisode(t *testing.T) {
	cases := []struct {
		filename string
		season   int
		episode  int
	}{
		{"Show.S02E05.mkv", 2, 5},
		{"series.s1e3.mp4", 1, 3},
		{"Show.S10E01.720p.mp4", 10, 1},
		{"Show.S01E00.mp4", 1, 0}, // season=1, episode=0
		{"nopattern.mp4", 1, 0},
		{"", 1, 0},
	}
	for _, c := range cases {
		got := parseFilenameHints(c.filename)
		if got.Season != c.season {
			t.Errorf("parseFilenameHints(%q).Season = %d, want %d", c.filename, got.Season, c.season)
		}
		if c.season > 0 && got.Episode != c.episode {
			t.Errorf("parseFilenameHints(%q).Episode = %d, want %d", c.filename, got.Episode, c.episode)
		}
	}
}

func TestParseFilenameHints_NoExtension(t *testing.T) {
	// Files without dots should still work.
	got := parseFilenameHints("ShowS03E07")
	if got.Season != 3 || got.Episode != 7 {
		t.Errorf("got {%d,%d}, want {3,7}", got.Season, got.Episode)
	}
}

func TestHintsForVideo_PrefersStoredFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "whatever.mp4")
	srv.store.UpdateVideoFields(ctx, v.ID, store.VideoFields{SeasonNumber: 3, EpisodeNumber: 7})
	v, _ = srv.store.GetVideo(ctx, v.ID)

	got := hintsForVideo(v, v.FilePath())
	if got.Season != 3 || got.Episode != 7 {
		t.Errorf("hintsForVideo with stored fields: got {%d,%d}, want {3,7}", got.Season, got.Episode)
	}
}

func TestHintsForVideo_FallsBackToFilename(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	// Filename has an SxxExx pattern but no DB fields set.
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "Show.S04E02.mp4")
	// filePath doesn't exist so metadata.Read will fail → fallback to filename.
	got := hintsForVideo(v, "/nonexistent/Show.S04E02.mp4")
	if got.Season != 4 || got.Episode != 2 {
		t.Errorf("hintsForVideo filename fallback: got {%d,%d}, want {4,2}", got.Season, got.Episode)
	}
}

func TestTmdbResult_DisplayTitle_Title(t *testing.T) {
	r := tmdbResult{Title: "My Movie", Name: "Ignored"}
	if got := r.DisplayTitle(); got != "My Movie" {
		t.Errorf("DisplayTitle() = %q, want My Movie", got)
	}
}

func TestTmdbResult_DisplayTitle_FallsBackToName(t *testing.T) {
	r := tmdbResult{Name: "My Show"}
	if got := r.DisplayTitle(); got != "My Show" {
		t.Errorf("DisplayTitle() = %q, want My Show", got)
	}
}

func TestTmdbResult_Year_FromReleaseDate(t *testing.T) {
	r := tmdbResult{ReleaseDate: "2023-07-15"}
	if got := r.Year(); got != "2023" {
		t.Errorf("Year() = %q, want 2023", got)
	}
}

func TestTmdbResult_Year_FromFirstAirDate(t *testing.T) {
	r := tmdbResult{FirstAirDate: "2021-01-10"}
	if got := r.Year(); got != "2021" {
		t.Errorf("Year() = %q, want 2021", got)
	}
}

func TestTmdbResult_Year_Empty(t *testing.T) {
	r := tmdbResult{}
	if got := r.Year(); got != "" {
		t.Errorf("Year() = %q, want empty", got)
	}
}

func TestHandleRemoveVideoTag_BadTagID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/videos/"+itoa(v.ID)+"/tags/notanid", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid tag id, got %d", rec.Code)
	}
}

func TestHandleAddVideoTag_EmptyName(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	form := url.Values{"tag": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/videos/"+itoa(v.ID)+"/tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty tag name, got %d", rec.Code)
	}
}
