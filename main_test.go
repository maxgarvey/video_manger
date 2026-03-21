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

// withMockTMDB spins up a mock TMDB HTTP server, overrides tmdbClient to
// redirect to it, and returns a cleanup function to restore the original.
func withMockTMDB(t *testing.T, handler http.HandlerFunc) func() {
	t.Helper()
	mock := httptest.NewServer(handler)
	orig := tmdbClient
	tmdbClient = &http.Client{Transport: &tmdbRoundTripper{host: strings.TrimPrefix(mock.URL, "http://")}}
	return func() {
		tmdbClient = orig
		mock.Close()
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
//
//	dir1/show1/s01e01.mp4  (TV, show "Alpha", season 1, episode 1, duration 600)
//	dir1/show1/s01e02.mp4  (TV, show "Alpha", season 1, episode 2, duration 1200)
//	dir1/show1/s02e01.mp4  (TV, show "Alpha", season 2, episode 1)
//	dir1/movie.mp4         (Movie, duration 5400)
func seedAPIFixture(t *testing.T, srv *server) {
	t.Helper()
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/lib")

	addEp := func(filename, show string, season, ep int, dur float64, vtype string) store.Video {
		v, err := srv.store.UpsertVideo(ctx, d.ID, d.Path, filename)
		if err != nil {
			t.Fatalf("UpsertVideo %s: %v", filename, err)
		}
		srv.store.SetExclusiveSystemTag(ctx, v.ID, "show", show)  //nolint:errcheck
		srv.store.SetExclusiveSystemTag(ctx, v.ID, "type", vtype) //nolint:errcheck
		srv.store.UpdateVideoFields(ctx, v.ID, store.VideoFields{  //nolint:errcheck
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

// --- handleYTDLPJobEvents SSE loop ---

func TestHandleYTDLPJobEvents_JobNotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/nonexistent-job-id/events", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown job, got %d", rec.Code)
	}
}

func TestHandleYTDLPJobEvents_UnknownJob(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/nonexistentjob/events", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing YTDLP job, got %d", rec.Code)
	}
}

func TestHandleYTDLPJobEvents_SendsDoneEvent(t *testing.T) {
	srv := newTestServer(t)
	jobID := "test-ytdlp-done"
	ch := make(chan string, 2)
	ch <- "Downloading"
	close(ch)
	job := &ytdlpJob{ch: ch}
	srv.jobsMu.Lock()
	srv.jobs[jobID] = job
	srv.jobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/"+jobID+"/events", nil)
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Downloading") {
		t.Errorf("expected progress line in SSE output, got: %s", body)
	}
	if !strings.Contains(body, "done") {
		t.Errorf("expected done event in SSE output, got: %s", body)
	}
}

func TestHandleYTDLPJobEvents_WithVideoID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, "/videos")
	v, _ := srv.store.UpsertVideo(ctx, d.ID, d.Path, "download.mp4")
	srv.store.UpdateVideoName(ctx, v.ID, "My Download")

	jobID := "test-ytdlp-video"
	ch := make(chan string)
	close(ch)
	job := &ytdlpJob{ch: ch, videoID: v.ID}
	srv.jobsMu.Lock()
	srv.jobs[jobID] = job
	srv.jobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/"+jobID+"/events", nil)
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "videoReady") {
		t.Errorf("expected videoReady event, got: %s", body)
	}
}

func TestHandleYTDLPJobEvents_SendsErrorEvent(t *testing.T) {
	srv := newTestServer(t)
	jobID := "test-ytdlp-err"
	ch := make(chan string)
	close(ch)
	job := &ytdlpJob{ch: ch, err: fmt.Errorf("download failed")}
	srv.jobsMu.Lock()
	srv.jobs[jobID] = job
	srv.jobsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ytdlp/job/"+jobID+"/events", nil)
	srv.routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "downloadError") && !strings.Contains(body, "error") {
		t.Errorf("expected error event in SSE output, got: %s", body)
	}
}
