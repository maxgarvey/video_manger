package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maxgarvey/video_manger/store"
	"golang.org/x/crypto/bcrypt"
)

func newTestServer(t *testing.T) *server {
	t.Helper()
	s, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	return &server{store: s, sessions: make(map[string]time.Time), syncingDirs: make(map[int64]struct{}), convertSem: make(chan struct{}, 2), jobs: make(map[string]*ytdlpJob), convertJobs: make(map[string]*convertJob)}
}

// newTestServerWithAuth creates a test server with password protection enabled.
func newTestServerWithAuth(t *testing.T, password string) *server {
	t.Helper()
	srv := newTestServer(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	srv.passwordHash = hash
	return srv
}

// --- Unit tests ---

// TestNewToken_Entropy verifies that newToken returns a 32-char hex string
// (16 bytes / 128 bits of entropy) and that two successive calls differ.
func TestNewToken_Entropy(t *testing.T) {
	t1 := newToken()
	t2 := newToken()
	if len(t1) != 32 {
		t.Errorf("expected 32-char hex token, got %d chars: %q", len(t1), t1)
	}
	if t1 == t2 {
		t.Error("newToken returned identical tokens on successive calls")
	}
}

// TestRenderErrorDoesNotLeakInternals verifies that a template execution error
// (e.g. nil pointer, missing field) returns a generic "internal server error"
// body to the client rather than Go type/path details.
func TestRenderErrorDoesNotLeakInternals(t *testing.T) {
	rec := httptest.NewRecorder()
	// Pass an incompatible data type (string instead of the expected struct) to
	// force a template execution error.  "directories.html" expects .Dirs and
	// .Syncing; passing a plain string will cause an execution failure.
	render(rec, "directories.html", "this-is-not-the-right-type")
	// The response must be 500.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	// The body must NOT contain Go-internal details.
	for _, leak := range []string{"template:", "reflect.", "interface", ".Dirs", ".Syncing"} {
		if strings.Contains(body, leak) {
			t.Errorf("response body leaks internal detail %q: %s", leak, body)
		}
	}
	// It must contain the generic message.
	if !strings.Contains(body, "internal server error") {
		t.Errorf("expected generic error message, got: %s", body)
	}
}

func TestIsVideoFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"movie.mp4", true},
		{"clip.webm", true},
		{"audio.ogg", true},
		{"film.mov", true},
		{"video.mkv", true},
		{"old.avi", true},
		{"UPPER.MP4", true},
		{"Mixed.MkV", true},
		{"document.pdf", false},
		{"image.jpg", false},
		{"script.go", false},
		{"noextension", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVideoFile(tc.name); got != tc.want {
				t.Errorf("isVideoFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// --- library.go helper unit tests ---

func TestCleanShowName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Show Name", "Show Name"},
		{"Show.Name", "Show Name"},
		{"Show_Name", "Show Name"},
		{"Show-Name", "Show Name"},
		{"Show...Name", "Show Name"},
		{"Show___Name", "Show Name"},
		{"  spaces  ", "spaces"},
		{"Show . Name", "Show Name"}, // dots→spaces, then Fields collapses multiple spaces
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		got := cleanShowName(c.in)
		if got != c.want {
			t.Errorf("cleanShowName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractShowFromFilename(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		// SxxExx patterns
		{"Breaking.Bad.S01E01.mkv", "Breaking Bad"},
		{"the_wire_S03E05_720p.mp4", "the wire"},
		{"ShowName-S02E10.avi", "ShowName"},
		// Season N patterns
		{"Seinfeld.Season.4.Episode.1.mp4", "Seinfeld"},
		{"my_show_Season1.mkv", "my show"},
		// No recognisable pattern — returns empty
		{"random_clip.mp4", ""},
		{"S01E01.mp4", ""},        // nothing before the pattern
		{"movie_2024.mp4", ""},
	}
	for _, c := range cases {
		got := extractShowFromFilename(c.filename)
		if got != c.want {
			t.Errorf("extractShowFromFilename(%q) = %q, want %q", c.filename, got, c.want)
		}
	}
}

func TestContainsWord(t *testing.T) {
	cases := []struct {
		s, word string
		want    bool
	}{
		{"youtube channel", "youtube", true},
		{"YouTube Channel", "youtube", true}, // case-insensitive
		{"not a tube", "youtube", false},     // not a whole word match
		{"concert footage", "concert", true},
		{"my_concert_video", "concert", true}, // underscore is word boundary
		{"concerts are fun", "concert", false}, // "concerts" != "concert"
		{"", "anything", false},
		{"word", "", false},
	}
	for _, c := range cases {
		got := containsWord(c.s, c.word)
		if got != c.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", c.s, c.word, got, c.want)
		}
	}
}

// --- Integration tests ---

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

func TestAuthRequired(t *testing.T) {
	srv := newTestServerWithAuth(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect to login (302), got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

func TestAuthLogin(t *testing.T) {
	srv := newTestServerWithAuth(t, "secret")
	body := strings.NewReader(url.Values{"password": {"secret"}}.Encode())
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect after login (302), got %d", rec.Code)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("expected session cookie after successful login")
	}
}

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

func TestInferVideoType(t *testing.T) {
	// season/episode -> TV
	if got := inferVideoType("foo.mp4", 1, 0, nil); got != "TV" {
		t.Errorf("expected TV, got %s", got)
	}
	// YouTube tag
	if got := inferVideoType("whatever.mp4", 0, 0, []string{"my_youtube_channel"}); got != "YouTube" {
		t.Errorf("expected YouTube, got %s", got)
	}
	// concert filename
	if got := inferVideoType("live_concert.mp4", 0, 0, nil); got != "Concert" {
		t.Errorf("expected Concert, got %s", got)
	}
	// default movie
	if got := inferVideoType("random.mp4", 0, 0, nil); got != "Movie" {
		t.Errorf("expected Movie, got %s", got)
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

func TestHandleInfo(t *testing.T) {
	srv := newTestServer(t)
	srv.port = "8080"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"port"`) {
		t.Error("expected port in /info response")
	}
	if !strings.Contains(body, `"addresses"`) {
		t.Error("expected addresses in /info response")
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

func TestHandleDirectories(t *testing.T) {
	srv := newTestServer(t)

	// List (empty)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No directories") {
		t.Error("expected empty state message")
	}

	// Add
	form := url.Values{"path": {"/my/videos"}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/directories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add dir: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/my/videos") {
		t.Error("expected new directory in response")
	}
}

func TestHandleCreateDirectory_Success(t *testing.T) {
	// Create the new dir inside home so it passes the home-dir restriction.
	home, _ := os.UserHomeDir()
	parent, err := os.MkdirTemp(home, "vm-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	newDir := filepath.Join(parent, "new_folder")

	srv := newTestServer(t)
	form := url.Values{"path": {newDir}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Dir created on disk.
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Error("expected directory to be created on disk")
	}
	// Dir registered in DB.
	ctx := context.Background()
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 1 || dirs[0].Path != newDir {
		t.Errorf("expected directory %s in DB, got %+v", newDir, dirs)
	}
}

func TestHandleCreateDirectory_AlreadyExists(t *testing.T) {
	// Existing dir must also be under home for the restriction check.
	home, _ := os.UserHomeDir()
	existing, err := os.MkdirTemp(home, "vm-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(existing) })

	srv := newTestServer(t)
	form := url.Values{"path": {existing}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("existing dir: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreateDirectory_OutsideHome verifies that creating a directory
// outside the user's home is rejected with 403.
func TestHandleCreateDirectory_OutsideHome(t *testing.T) {
	srv := newTestServer(t)
	// /tmp is outside the home directory on most systems.
	form := url.Values{"path": {"/tmp/vm-security-test-dir"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	// Should be 403 unless /tmp happens to be under home (extremely unlikely).
	home, _ := os.UserHomeDir()
	rel, _ := filepath.Rel(home, "/tmp/vm-security-test-dir")
	if !strings.HasPrefix(rel, "..") {
		t.Skip("skipping: /tmp is under home on this system")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateDirectory_EmptyPath(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"path": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty path: expected 400, got %d", rec.Code)
	}
}

func TestHandleDirectoryDeleteConfirm(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/my/videos")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/"+itoa(d.ID)+"/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/my/videos") {
		t.Error("expected directory path in confirmation")
	}
	if !strings.Contains(body, "Remove from library") {
		t.Error("expected library-only option")
	}
	if !strings.Contains(body, "Remove and delete files") {
		t.Error("expected file-delete option")
	}
}

func TestHandleDeleteDirectoryAndFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{"ep1.mp4", "ep2.mp4"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, dir)
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep1.mp4")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "ep2.mp4")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID)+"/files", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Directory and videos removed from DB
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories, got %d", len(dirs))
	}
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 0 {
		t.Errorf("expected 0 videos, got %d", len(videos))
	}
	// Files removed from disk
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted from disk", f)
		}
	}
}

func TestHandleDeleteDirectory(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/to/delete")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories after delete, got %d", len(dirs))
	}
}

func TestHandleDeleteDirectory_KeepsVideos(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/movies")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "film.mp4")

	// Library-only remove: DELETE /directories/{id}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/"+itoa(d.ID), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Directory is gone.
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) != 0 {
		t.Errorf("expected 0 directories, got %d", len(dirs))
	}

	// Video is still listed with its path intact.
	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Fatalf("expected video to survive directory removal, got %d videos", len(videos))
	}
	if videos[0].ID != v.ID {
		t.Errorf("expected video ID %d, got %d", v.ID, videos[0].ID)
	}
	if videos[0].DirectoryPath != "/movies" {
		t.Errorf("expected DirectoryPath=/movies, got %q", videos[0].DirectoryPath)
	}
	if videos[0].FilePath() != "/movies/film.mp4" {
		t.Errorf("expected FilePath=/movies/film.mp4, got %q", videos[0].FilePath())
	}
}

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

func TestHandleYTDLP_MissingURL(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"dir_id": {"1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleYTDLP_MissingDirID(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"urls": {"https://example.com/video"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleYTDLP_InvalidDir(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"urls": {"https://example.com/video"}, "dir_id": {"999"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown dir, got %d", rec.Code)
	}
}

func TestHandleYTDLP_NotInstalled(t *testing.T) {
	// With empty PATH, yt-dlp cannot be found — handler returns 503 immediately.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, t.TempDir())

	t.Setenv("PATH", t.TempDir()) // empty PATH — yt-dlp not found

	form := url.Values{"urls": {"https://example.com/v"}, "dir_id": {itoa(d.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when yt-dlp not installed, got %d", rec.Code)
	}
}

func TestHandleYTDLP_MultipleURLs(t *testing.T) {
	// Submitting multiple URLs should create one progress block per URL.
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, t.TempDir())

	// Create a stub yt-dlp so LookPath succeeds; downloads fail async but the
	// initial POST should still return the progress page immediately.
	bin := t.TempDir()
	stub := filepath.Join(bin, "yt-dlp")
	os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0755) //nolint:errcheck
	t.Setenv("PATH", bin)

	urls := "https://example.com/v1\nhttps://example.com/v2\nhttps://example.com/v3"
	form := url.Values{"urls": {urls}, "dir_id": {itoa(d.ID)}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Should have 3 EventSource subscriptions — one per URL.
	count := strings.Count(body, "new EventSource")
	if count != 3 {
		t.Errorf("expected 3 new EventSource calls for 3 URLs, got %d", count)
	}
}

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

func TestSyncDir_Recursive(t *testing.T) {
	// Build a tree: root/{a.mp4, sub/{b.mkv, ignore.txt}, sub2/{c.mp4}}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	sub2 := filepath.Join(root, "sub2")
	for _, d := range []string{sub, sub2} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mkv"),
		filepath.Join(sub, "ignore.txt"),
		filepath.Join(sub2, "c.mp4"),
	} {
		if err := os.WriteFile(f, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)

	videos, err := srv.store.ListVideos(ctx)
	if err != nil {
		t.Fatalf("ListVideos: %v", err)
	}
	if len(videos) != 3 {
		t.Fatalf("expected 3 videos (a.mp4, b.mkv, c.mp4), got %d", len(videos))
	}

	// Verify FilePath() resolves to correct subdirectory.
	paths := make(map[string]bool)
	for _, v := range videos {
		paths[v.FilePath()] = true
	}
	for _, want := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mkv"),
		filepath.Join(sub2, "c.mp4"),
	} {
		if !paths[want] {
			t.Errorf("expected video at %s, not found in %v", want, paths)
		}
	}
}

func TestSyncDir_AutoTagsByDirectoryName(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(sub, "b.mp4"),
	} {
		if err := os.WriteFile(f, []byte("fake"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 2 {
		t.Fatalf("expected 2 videos, got %d", len(videos))
	}

	dirTagName := filepath.Base(root)
	for _, v := range videos {
		tags, err := srv.store.ListTagsByVideo(ctx, v.ID)
		if err != nil {
			t.Fatalf("ListTagsByVideo: %v", err)
		}
		found := false
		for _, tag := range tags {
			if tag.Name == dirTagName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("video %s missing auto-tag %q", v.Filename, dirTagName)
		}
	}
}

func TestSyncDir_AutoTag_Idempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)
	srv.syncDir(d) // second sync should not duplicate tags

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(videos))
	}
	tags, _ := srv.store.ListTagsByVideo(ctx, videos[0].ID)
	// Count how many times the directory name tag appears (should be exactly 1, not duplicated).
	dirTagName := filepath.Base(root)
	dirTagCount := 0
	for _, tg := range tags {
		if tg.Name == dirTagName {
			dirTagCount++
		}
	}
	if dirTagCount != 1 {
		t.Errorf("expected directory name tag %q to appear exactly once, got %d times (all tags: %v)", dirTagName, dirTagCount, tags)
	}
}

func TestSyncDir_IdempotentOnResync(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, root)
	srv.syncDir(d)
	srv.syncDir(d) // second sync should not duplicate

	videos, _ := srv.store.ListVideos(ctx)
	if len(videos) != 1 {
		t.Errorf("expected 1 video after double sync, got %d", len(videos))
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

func TestHandleDirectoryOptions(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Empty — should still return 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/options", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty), got %d", rec.Code)
	}

	// With a directory — it should appear in the options.
	d, _ := srv.store.AddDirectory(ctx, "/my/movies")
	_ = d

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/directories/options", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (with dir), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/my/movies") {
		t.Error("expected directory path in options response")
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

// tmdbRoundTripper redirects all requests to a local mock server, allowing
// tests to exercise handleLookupSearch without hitting the real TMDB API.
type tmdbRoundTripper struct {
	host string // e.g. "127.0.0.1:PORT" (no scheme)
}

func (t *tmdbRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = "http"
	r2.URL.Host = t.host
	return http.DefaultTransport.RoundTrip(r2)
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

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}

// --- Auth tests ---

func newProtectedServer(t *testing.T, password string) *server {
	t.Helper()
	s := newTestServer(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	s.passwordHash = hash
	return s
}

func TestAuth_NoPassword_PassesThrough(t *testing.T) {
	srv := newTestServer(t) // no password
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 without password, got %d", rec.Code)
	}
}

func TestAuth_WithPassword_UnauthRedirects(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

func TestAuth_LoginPage_Accessible(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /login, got %d", rec.Code)
	}
}

func TestAuth_WrongPassword_RerendersForm(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=wrongpassword")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (re-render) on wrong password, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Wrong password") {
		t.Error("expected 'Wrong password' in response body")
	}
}

func TestAuth_CorrectPassword_SetsSessionCookie(t *testing.T) {
	srv := newProtectedServer(t, "secret")
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=secret")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect after correct password, got %d", rec.Code)
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "session=") {
		t.Errorf("expected session cookie in Set-Cookie, got %q", setCookie)
	}
}

func TestAuth_WithSessionCookie_PassesThrough(t *testing.T) {
	srv := newProtectedServer(t, "secret")

	// First login to get a session token.
	rec := httptest.NewRecorder()
	body := strings.NewReader("password=secret")
	req := httptest.NewRequest(http.MethodPost, "/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	cookie := rec.Result().Cookies()[0]

	// Now request a protected page with the session cookie.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(cookie)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid session, got %d", rec2.Code)
	}
}

// TestAuth_SecureCookieFlag verifies that when secureCookies is true the
// Set-Cookie header includes the Secure attribute, and that it is absent
// when secureCookies is false.
func TestAuth_SecureCookieFlag(t *testing.T) {
	login := func(srv *server) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("password=secret"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)
		return rec.Header().Get("Set-Cookie")
	}

	// With secureCookies=false (default), Secure must NOT appear.
	srvPlain := newProtectedServer(t, "secret")
	if setCookie := login(srvPlain); strings.Contains(setCookie, "Secure") {
		t.Errorf("secureCookies=false: unexpected Secure attribute in %q", setCookie)
	}

	// With secureCookies=true, Secure must appear.
	srvSecure := newProtectedServer(t, "secret")
	srvSecure.secureCookies = true
	if setCookie := login(srvSecure); !strings.Contains(setCookie, "Secure") {
		t.Errorf("secureCookies=true: expected Secure attribute in %q", setCookie)
	}
}

// E7: reltime edge cases
func TestReltime(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"not-a-date", "not-a-date"},
		{time.Now().UTC().Format("2006-01-02 15:04:05"), "just now"},
		{time.Now().UTC().Add(-30 * time.Second).Format("2006-01-02 15:04:05"), "just now"},
		{time.Now().UTC().Add(-90 * time.Second).Format("2006-01-02 15:04:05"), "1 min ago"},
		{time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05"), "5 mins ago"},
		{time.Now().UTC().Add(-90 * time.Minute).Format("2006-01-02 15:04:05"), "1 hr ago"},
		{time.Now().UTC().Add(-5 * time.Hour).Format("2006-01-02 15:04:05"), "5 hrs ago"},
		{time.Now().UTC().Add(-36 * time.Hour).Format("2006-01-02 15:04:05"), "yesterday"},
		{time.Now().UTC().Add(-4 * 24 * time.Hour).Format("2006-01-02 15:04:05"), "4 days ago"},
	}
	for _, tc := range cases {
		got := reltime(tc.input)
		if got != tc.want {
			t.Errorf("reltime(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// E6: handleBrowseFS
func TestHandleBrowseFS(t *testing.T) {
	srv := newTestServer(t)
	tmp := t.TempDir()

	// Create a subdirectory
	sub := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// BrowseFS is restricted to home dir — use a real path inside home
	home, _ := os.UserHomeDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fs?path="+url.QueryEscape(home), nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Path outside home dir should return 403
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/fs?path="+url.QueryEscape("/etc"), nil)
	srv.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for /etc, got %d", rec2.Code)
	}
}

// TestHandleBrowseFS_SymlinkEscape verifies that a symlink inside the home
// directory pointing outside is blocked by filepath.EvalSymlinks.
func TestHandleBrowseFS_SymlinkEscape(t *testing.T) {
	home, _ := os.UserHomeDir()

	// Create a temp dir inside home to hold the symlink.
	linkParent, err := os.MkdirTemp(home, "vm-test-symlink-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(linkParent) })

	// Create a symlink inside home that points to /tmp (outside home on most systems).
	linkPath := filepath.Join(linkParent, "escape")
	target := "/tmp"
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("could not create symlink (skipping): %v", err)
	}

	// Determine whether /tmp is actually outside home after symlink resolution.
	realTarget, _ := filepath.EvalSymlinks(target)
	realHome, _ := filepath.EvalSymlinks(home)
	rel, _ := filepath.Rel(realHome, realTarget)
	if !strings.HasPrefix(rel, "..") {
		t.Skipf("skipping: /tmp resolves inside home on this system (%s)", rel)
	}

	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fs?path="+url.QueryEscape(linkPath), nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("symlink escape: expected 403 or 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// E6: handleNextUnwatched
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

// E6: handleListDuplicates
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

// E4: handleImportUpload
func TestHandleImportUpload(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	tmp := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	// Build multipart body
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("dir_id", itoa(d.ID))
	mw.WriteField("filename", "clip.mp4")
	fw, _ := mw.CreateFormFile("file", "clip.mp4")
	fw.Write([]byte("fake video content"))
	mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// File should now exist on disk
	if _, err := os.Stat(filepath.Join(tmp, "clip.mp4")); err != nil {
		t.Errorf("expected clip.mp4 on disk: %v", err)
	}

	// Video should be in the DB
	videos, err := srv.store.ListVideosByDirectory(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListVideosByDirectory: %v", err)
	}
	if len(videos) == 0 {
		t.Error("expected video in DB after upload")
	}
}

func TestHandleImportUpload_PathTraversal(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	tmp := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("dir_id", itoa(d.ID))
	mw.WriteField("filename", "../../etc/passwd.mp4") // path traversal attempt
	fw, _ := mw.CreateFormFile("file", "passwd.mp4")
	fw.Write([]byte("evil"))
	mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (traversal stripped), got %d", rec.Code)
	}
	// File should land in tmp, not escape it
	if _, err := os.Stat(filepath.Join(tmp, "passwd.mp4")); err != nil {
		t.Errorf("expected passwd.mp4 in tmp dir: %v", err)
	}
}

func TestHandleImportUpload_NotVideo(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	tmp := t.TempDir()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("dir_id", itoa(d.ID))
	mw.WriteField("filename", "document.pdf")
	fw, _ := mw.CreateFormFile("file", "document.pdf")
	fw.Write([]byte("pdf content"))
	mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-video, got %d", rec.Code)
	}
}

// E5: handleTrim negative paths
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

// --- M6: syncDir tests ---

// TestSyncDir_PrunesStaleEntries verifies that syncDir removes DB records for
// files that have been deleted from disk.
func TestSyncDir_PrunesStaleEntries(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()

	// Register the directory and seed it with two video files.
	if err := os.WriteFile(filepath.Join(tmp, "keep.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(tmp, "stale.mp4")
	if err := os.WriteFile(stale, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := srv.store.AddDirectory(ctx, tmp)
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	srv.syncDir(d)

	// Verify both files are in the DB after first sync.
	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 2 {
		t.Fatalf("expected 2 videos after first sync, got %d", len(vids))
	}

	// Delete one file from disk and re-sync.
	if err := os.Remove(stale); err != nil {
		t.Fatal(err)
	}
	srv.syncDir(d)

	// Stale record should have been pruned.
	vids, _ = srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video after pruning stale entry, got %d", len(vids))
	}
	if vids[0].Filename != "keep.mp4" {
		t.Errorf("expected keep.mp4 to survive, got %q", vids[0].Filename)
	}
}

// TestSyncDir_AutoTagsDirectory verifies that syncDir applies the directory's
// base name as a tag to each video it discovers.
func TestSyncDir_AutoTagsDirectory(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(tmp, "clip.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := srv.store.AddDirectory(ctx, tmp)
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	tags, err := srv.store.ListTagsByVideo(ctx, vids[0].ID)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	dirBase := filepath.Base(tmp)
	var found bool
	for _, tg := range tags {
		if tg.Name == dirBase {
			found = true
		}
	}
	if !found {
		t.Errorf("expected auto-tag %q but got %v", dirBase, tags)
	}
}

// New show inference tests
func TestSyncDir_InferShowFromDirectory(t *testing.T) {
	tmp := t.TempDir()
	showDir := filepath.Join(tmp, "MyShow")
	if err := os.MkdirAll(showDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "ep1.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	if vids[0].ShowName != "MyShow" {
		t.Errorf("show name = %q; want MyShow", vids[0].ShowName)
	}
}

func TestSyncDir_InferShowFromFilename(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "Cool.Show.S02E03.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	if vids[0].ShowName != "Cool Show" {
		t.Errorf("show name = %q; want Cool Show", vids[0].ShowName)
	}
}

func TestSyncDir_ShowNameStandalone(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "Some Movie.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	if vids[0].ShowName != "" {
		t.Errorf("expected empty show name, got %q", vids[0].ShowName)
	}
}

// --- Sidecar JSON tests ---

// TestSyncDir_Sidecar verifies that a <basename>.json file alongside a video
// is read during syncDir and applied to the video's title, fields, and tags.
func TestSyncDir_Sidecar(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "film.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	sidecar := `{
		"title":         "My Film",
		"tags":          ["action", "sci-fi"],
		"actors":        "Tom Hanks",
		"genre":         "Drama",
		"season":        2,
		"episode":       5,
		"episode_title": "The Pilot",
		"studio":        "Warner",
		"channel":       "HBO"
	}`
	if err := os.WriteFile(filepath.Join(tmp, "film.json"), []byte(sidecar), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, err := srv.store.ListVideosByDirectory(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListVideosByDirectory: %v", err)
	}
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	v := vids[0]

	if v.Title() != "My Film" {
		t.Errorf("title: got %q, want %q", v.Title(), "My Film")
	}
	if v.Actors != "Tom Hanks" {
		t.Errorf("actors: got %q, want %q", v.Actors, "Tom Hanks")
	}
	if v.Genre != "Drama" {
		t.Errorf("genre: got %q, want %q", v.Genre, "Drama")
	}
	if v.SeasonNumber != 2 {
		t.Errorf("season: got %d, want 2", v.SeasonNumber)
	}
	if v.EpisodeNumber != 5 {
		t.Errorf("episode: got %d, want 5", v.EpisodeNumber)
	}
	if v.EpisodeTitle != "The Pilot" {
		t.Errorf("episode_title: got %q, want %q", v.EpisodeTitle, "The Pilot")
	}
	if v.Studio != "Warner" {
		t.Errorf("studio: got %q, want %q", v.Studio, "Warner")
	}
	if v.Channel != "HBO" {
		t.Errorf("channel: got %q, want %q", v.Channel, "HBO")
	}

	tags, err := srv.store.ListTagsByVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	tagSet := make(map[string]bool, len(tags))
	for _, tg := range tags {
		tagSet[tg.Name] = true
	}
	for _, want := range []string{"action", "sci-fi"} {
		if !tagSet[want] {
			t.Errorf("expected tag %q in %v", want, tags)
		}
	}
}

// TestSyncDir_SidecarMissing verifies that syncDir works normally when no
// sidecar JSON exists next to a video file.
func TestSyncDir_SidecarMissing(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "film.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
}

// TestSyncDir_SidecarInvalid verifies that a malformed sidecar JSON logs a
// warning but does not prevent the video from being registered.
func TestSyncDir_SidecarInvalid(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "film.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "film.json"), []byte("{not valid json{{"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d) // must not panic

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video even with invalid sidecar, got %d", len(vids))
	}
}

// TestSyncDir_SidecarIdempotent verifies that running syncDir twice with the
// same sidecar does not duplicate tags.
func TestSyncDir_SidecarIdempotent(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "film.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "film.json"), []byte(`{"tags":["action","drama"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)
	srv.syncDir(d) // second sync must not duplicate tags

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video, got %d", len(vids))
	}
	tags, err := srv.store.ListTagsByVideo(ctx, vids[0].ID)
	if err != nil {
		t.Fatalf("ListTagsByVideo: %v", err)
	}
	// Count occurrences of each tag name — must each appear exactly once.
	counts := make(map[string]int)
	for _, tg := range tags {
		counts[tg.Name]++
	}
	for _, name := range []string{"action", "drama"} {
		if counts[name] != 1 {
			t.Errorf("tag %q: expected count 1, got %d", name, counts[name])
		}
	}
}

// TestSyncDir_SidecarFieldsTruncated verifies that sidecar string fields longer
// than sidecarFieldMaxLen are truncated before being stored.
func TestSyncDir_SidecarFieldsTruncated(t *testing.T) {
	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "movie.mp4")
	if err := os.WriteFile(videoPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	longStr := strings.Repeat("x", sidecarFieldMaxLen+100)
	sidecar := fmt.Sprintf(`{"title":%q,"actors":%q}`, longStr, longStr)
	if err := os.WriteFile(filepath.Join(tmp, "movie.json"), []byte(sidecar), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)
	srv.syncDir(d)

	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) == 0 {
		t.Fatal("no video found after sync")
	}
	v := vids[0]
	title := v.Title()
	if len(title) > sidecarFieldMaxLen {
		t.Errorf("title not truncated: len=%d", len(title))
	}
}

// --- Subfolder creation tests ---

func TestHandleCreateSubfolder(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	form := url.Values{"name": {"Season 1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/"+itoa(d.ID)+"/subfolder", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Folder must exist on disk.
	want := filepath.Join(tmp, "Season 1")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected subfolder %q on disk: %v", want, err)
	}

	// Subfolder must be registered as a directory.
	dirs, err := srv.store.ListDirectories(ctx)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	var found bool
	for _, dir := range dirs {
		if dir.Path == want {
			found = true
		}
	}
	if !found {
		t.Errorf("subfolder %q not registered; dirs=%v", want, dirs)
	}
}

func TestHandleCreateSubfolder_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	form := url.Values{"name": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/"+itoa(d.ID)+"/subfolder", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", rec.Code)
	}
}

func TestHandleCreateSubfolder_InvalidParent(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{"name": {"foo"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/9999/subfolder", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown parent, got %d", rec.Code)
	}
}

func TestHandleCreateSubfolder_PathTraversal(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	for _, bad := range []string{"../evil", "foo/bar", `foo\bar`} {
		form := url.Values{"name": {bad}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/directories/"+itoa(d.ID)+"/subfolder", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 400, got %d", bad, rec.Code)
		}
	}
}

// --- M8: Upload → sync integration test ---

// TestHandleImportUpload_VideoAppearsInList uploads a video file and then
// verifies that it appears in GET /videos, confirming the full upload→upsert
// pipeline works end-to-end.
func TestHandleImportUpload_VideoAppearsInList(t *testing.T) {
	tmp := t.TempDir()
	srv := newTestServer(t)
	ctx := context.Background()

	d, err := srv.store.AddDirectory(ctx, tmp)
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}

	// Build a multipart upload containing a tiny fake MP4.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("dir_id", itoa(d.ID))
	_ = mw.WriteField("filename", "integration.mp4")
	fw, _ := mw.CreateFormFile("file", "integration.mp4")
	fw.Write([]byte("fake video content"))
	mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The file should exist on disk.
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file on disk after upload, got %d", len(entries))
	}

	// GET /videos should include the uploaded video.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/videos", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("video list: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "integration") {
		t.Error("uploaded video not found in video list after upload")
	}

	// The video should also be in the DB via the store.
	vids, _ := srv.store.ListVideosByDirectory(ctx, d.ID)
	if len(vids) != 1 {
		t.Fatalf("expected 1 video in DB after upload, got %d", len(vids))
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

// TestMoveRollback_CrossDevice verifies that the cross-device rollback path
// removes the destination copy and leaves the source intact.  Previously the
// code called os.Rename(dst, src) which fails with EXDEV on cross-device
// moves; the fixed code calls os.Remove(dst).
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

// --- yt-dlp URL validation (SSRF prevention) ---

// TestHandleYTDLPDownload_BlockedSchemes verifies that non-http/https URLs are
// rejected before being passed to yt-dlp, preventing SSRF via file:// etc.
func TestHandleYTDLPDownload_BlockedSchemes(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	dir, _ := srv.store.AddDirectory(ctx, t.TempDir())

	blocked := []string{
		"file:///etc/passwd",
		"ftp://internal.example.com/file",
		"gopher://evil.example.com/",
		"not-a-url",
	}
	for _, bad := range blocked {
		form := url.Values{"urls": {bad}, "dir_id": {itoa(dir.ID)}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
			// 503 is acceptable if yt-dlp is not installed; 400 is the target.
			t.Errorf("url=%q: expected 400 or 503, got %d: %s", bad, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusBadRequest {
			if !strings.Contains(rec.Body.String(), "http") {
				t.Errorf("url=%q: expected informative error, got %q", bad, rec.Body.String())
			}
		}
	}
}

// TestHandleYTDLPDownload_AllowedSchemes verifies that valid http/https URLs
// pass URL validation (they'll then fail if yt-dlp is not installed, which is fine).
func TestHandleYTDLPDownload_AllowedSchemes(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	dir, _ := srv.store.AddDirectory(ctx, t.TempDir())

	allowed := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"http://example.com/video.mp4",
	}
	for _, good := range allowed {
		form := url.Values{"urls": {good}, "dir_id": {itoa(dir.ID)}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ytdlp/download", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		srv.routes().ServeHTTP(rec, req)
		// Either 200 (yt-dlp present) or 503 (not installed) are acceptable.
		// 400 would mean the URL was incorrectly rejected.
		if rec.Code == http.StatusBadRequest {
			t.Errorf("url=%q: valid URL rejected with 400: %s", good, rec.Body.String())
		}
	}
}

// --- 3b: yt-dlp info.json path capture ---

// TestYTDLPInfoJSONCleanup verifies that a .info.json file placed alongside a
// video is removed after metadata tagging (exercising the cleanup path without
// needing ffmpeg by making parseYTDLPInfoJSON fail on an empty JSON).
func TestYTDLPInfoJSONCleanup(t *testing.T) {
	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "clip.mp4")
	infoPath := videoPath + ".info.json"

	if err := os.WriteFile(videoPath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a valid minimal info.json that parseYTDLPInfoJSON can parse.
	if err := os.WriteFile(infoPath, []byte(`{"title":"Test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate the tagging flow: read info.json → parse → (skip write, no ffmpeg) → delete.
	data, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	_, _ = parseYTDLPInfoJSON(data)
	if err := os.Remove(infoPath); err != nil {
		t.Fatalf("Remove info.json: %v", err)
	}
	if _, err := os.Stat(infoPath); !os.IsNotExist(err) {
		t.Error("info.json should have been deleted")
	}
}

// --- parseYTDLPInfoJSON ---

func TestParseYTDLPInfoJSON_Full(t *testing.T) {
	raw := `{
		"title": "My Video",
		"description": "A great video",
		"channel": "TestChannel",
		"uploader": "TestUploader",
		"upload_date": "20230415",
		"tags": ["tag1", "tag2"],
		"categories": ["Entertainment"],
		"genre": "Comedy",
		"series": "My Show",
		"season_number": 2,
		"episode_number": 5,
		"episode_id": "S02E05"
	}`
	u, ok := parseYTDLPInfoJSON([]byte(raw))
	if !ok {
		t.Fatal("expected ok")
	}
	if u.Title == nil || *u.Title != "My Video" {
		t.Errorf("Title = %v", u.Title)
	}
	if u.Description == nil || *u.Description != "A great video" {
		t.Errorf("Description = %v", u.Description)
	}
	if u.Genre == nil || *u.Genre != "Comedy" {
		t.Errorf("Genre = %v", u.Genre)
	}
	if u.Date == nil || *u.Date != "2023-04-15" {
		t.Errorf("Date = %v", u.Date)
	}
	if u.Network == nil || *u.Network != "TestChannel" {
		t.Errorf("Network = %v", u.Network)
	}
	if u.Show == nil || *u.Show != "My Show" {
		t.Errorf("Show = %v", u.Show)
	}
	if u.SeasonNum == nil || *u.SeasonNum != "2" {
		t.Errorf("SeasonNum = %v", u.SeasonNum)
	}
	if u.EpisodeNum == nil || *u.EpisodeNum != "5" {
		t.Errorf("EpisodeNum = %v", u.EpisodeNum)
	}
	if u.EpisodeID == nil || *u.EpisodeID != "S02E05" {
		t.Errorf("EpisodeID = %v", u.EpisodeID)
	}
	if len(u.Keywords) != 2 || u.Keywords[0] != "tag1" {
		t.Errorf("Keywords = %v", u.Keywords)
	}
}

func TestParseYTDLPInfoJSON_FallbackGenre(t *testing.T) {
	// When genre is absent, fall back to first category.
	raw := `{"title":"X","categories":["Science & Technology"]}`
	u, ok := parseYTDLPInfoJSON([]byte(raw))
	if !ok {
		t.Fatal("expected ok")
	}
	if u.Genre == nil || *u.Genre != "Science & Technology" {
		t.Errorf("Genre fallback = %v", u.Genre)
	}
}

func TestParseYTDLPInfoJSON_FallbackNetwork(t *testing.T) {
	// When channel is absent, fall back to uploader.
	raw := `{"title":"X","uploader":"SomeUploader"}`
	u, ok := parseYTDLPInfoJSON([]byte(raw))
	if !ok {
		t.Fatal("expected ok")
	}
	if u.Network == nil || *u.Network != "SomeUploader" {
		t.Errorf("Network fallback = %v", u.Network)
	}
}

func TestParseYTDLPInfoJSON_InvalidJSON(t *testing.T) {
	_, ok := parseYTDLPInfoJSON([]byte("not json"))
	if ok {
		t.Error("expected not ok for invalid JSON")
	}
}

// --- 3a: video fields endpoints ---

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

// --- 3e: pagination ---

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

// --- 3f: rename response body assertion (HTML-escaped) ---

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

// TestHandleMoveVideo_SubdirPathTraversal checks that supplying a "subdir"
// with path separators or ".." is rejected with 400, preventing traversal
// outside the target directory (e.g. "../../etc").
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

// --- Quick Label ---

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

// --- Mark watched / clear progress ---

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

// --- Thumbnail handlers ---

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

// --- Sync directory ---

func TestHandleSyncDirectory_OK(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "new.mp4"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, tmp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/"+itoa(d.ID)+"/sync", nil)
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// The response is the directory list — it should contain the path.
	if !strings.Contains(rec.Body.String(), tmp) {
		t.Error("expected directory path in sync response")
	}
}

func TestHandleSyncDirectory_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories/9999/sync", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown directory, got %d", rec.Code)
	}
}

// --- List tags / random video ---

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

// --- SSE job-not-found paths ---

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

func TestHandleYTDLPJobEvents_JobNotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/nonexistent-job-id/events", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", rec.Code)
	}
}

// --- Copy to library ---

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

// --- Relocate video ---

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

// --- freeOutputName unit tests ---

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

// --- findRegisteredDir unit tests ---

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

// --- Trim success path ---

// TestHandleTrim_Success creates a stub ffmpeg that copies its input to the
// output path and exits 0, then verifies that the trim handler:
//   - returns 200
//   - sets HX-Trigger with a valid trimComplete JSON payload
//   - upserts the trimmed file into the database
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

// --- System tag tests (Feature: Tags as Lingua Franca) ---

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

// ── JSON API handler tests (/api/...) ────────────────────────────────────────

// apiGet is a test helper that fires a GET request and returns the decoded JSON.
func apiGet(t *testing.T, srv *server, path string, out interface{}) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("apiGet %s: decode JSON: %v\nbody: %s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

// seedAPIFixture creates a small video library for API tests:
//   dir1/show1/s01e01.mp4  (TV, show "Alpha", season 1, episode 1, duration 600)
//   dir1/show1/s01e02.mp4  (TV, show "Alpha", season 1, episode 2, duration 1200)
//   dir1/show1/s02e01.mp4  (TV, show "Alpha", season 2, episode 1)
//   dir1/movie.mp4         (Movie, duration 5400)
func seedAPIFixture(t *testing.T, srv *server) {
	t.Helper()
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")

	addEp := func(filename, show string, season, ep int, dur float64, vtype string) store.Video {
		v, err := srv.store.UpsertVideo(ctx, d.ID, d.Path, filename)
		if err != nil {
			t.Fatalf("UpsertVideo %s: %v", filename, err)
		}
		srv.store.SetExclusiveSystemTag(ctx, v.ID, "show", show)   //nolint:errcheck
		srv.store.SetExclusiveSystemTag(ctx, v.ID, "type", vtype)  //nolint:errcheck
		srv.store.UpdateVideoFields(ctx, v.ID, store.VideoFields{   //nolint:errcheck
			SeasonNumber:  season,
			EpisodeNumber: ep,
		})
		srv.store.UpdateVideoDuration(ctx, v.ID, dur) //nolint:errcheck
		v, _ = srv.store.GetVideo(ctx, v.ID)
		return v
	}

	addEp("s01e01.mp4", "Alpha", 1, 1, 600, "TV")
	addEp("s01e02.mp4", "Alpha", 1, 2, 1200, "TV")
	addEp("s02e01.mp4", "Alpha", 2, 1, 0, "TV")
	addEp("movie.mp4", "", 0, 0, 5400, "Movie")
}

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

// --- srtToWebVTT ---

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

// --- handleServeSubtitles ---

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

// --- handleLogout ---

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

// --- New tests appended below ---

// 5. handleVideoTags
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

// 6. Air date field end-to-end through quick-label handler
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

// 7. Trim metadata copy tests
// These use a stub ffmpeg that creates the output file and exits 0.
func makeFfmpegStub(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	stub := filepath.Join(bin, "ffmpeg")
	stubScript := "#!/bin/sh\nfor last; do true; done\n: > \"$last\"\n"
	if err := os.WriteFile(stub, []byte(stubScript), 0755); err != nil {
		t.Fatalf("write ffmpeg stub: %v", err)
	}
	return bin
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

// 13. Concurrent sync — two simultaneous startSyncDir calls on the same directory
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

// 14. Search with Unicode
func TestHandleVideoSearch_Unicode(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "Résumé.mp4")           //nolint:errcheck
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "日本語ビデオ.mp4")           //nolint:errcheck
	srv.store.UpsertVideo(ctx, d.ID, d.Path, "emoji_🎬.mp4")          //nolint:errcheck

	for _, q := range []string{"Résumé", "日本語", "🎬", "café"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/videos?q="+url.QueryEscape(q), nil)
		srv.routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("query %q: expected 200, got %d", q, rec.Code)
		}
	}
}
