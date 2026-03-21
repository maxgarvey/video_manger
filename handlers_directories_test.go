package main

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestHandleDeleteDirectory_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/directories/notanid", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404, got %d", rec.Code)
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

func TestHandleDirectoryDeleteConfirm_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/99999/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent directory, got %d", rec.Code)
	}
}

func TestHandleDirectoryDeleteConfirm_BadID(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/directories/notanid/delete-confirm", nil)
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404 for bad ID, got %d", rec.Code)
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

func TestHandleImportUpload_BadDirID(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"dir_id": {"notanid"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad dir_id, got %d", rec.Code)
	}
}

func TestHandleImportUpload_DirNotFound(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"dir_id": {"99999"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing directory, got %d", rec.Code)
	}
}

func TestHandleImportUpload_NoFile(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()
	d, _ := srv.store.AddDirectory(ctx, t.TempDir())

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("dir_id", itoa(d.ID))   //nolint:errcheck
	mw.WriteField("filename", "test.mp4") //nolint:errcheck
	mw.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file, got %d", rec.Code)
	}
}

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

func TestHandleAddDirectory_Success(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	form := url.Values{"path": {dir}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Directory should now be in the store.
	ctx := context.Background()
	dirs, _ := srv.store.ListDirectories(ctx)
	if len(dirs) == 0 {
		t.Error("expected at least one directory after addDirectory")
	}
}

func TestHandleAddDirectory_EmptyPath(t *testing.T) {
	srv := newTestServer(t)
	form := url.Values{"path": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/directories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d", rec.Code)
	}
}
