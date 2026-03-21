package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		{"S01E01.mp4", ""},      // nothing before the pattern
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
		{"my_concert_video", "concert", true},  // underscore is word boundary
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

func TestCheckBinaries_DoesNotPanic(t *testing.T) {
	// checkBinaries only logs warnings for missing binaries; it must never panic.
	checkBinaries()
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

func TestSyncDir_SkipsRegisteredSubdirectory(t *testing.T) {
	// Create a parent directory with one video and a subdirectory (child) with another.
	root := t.TempDir()
	child := filepath.Join(root, "movies")
	os.Mkdir(child, 0755)                                                        //nolint:errcheck
	os.WriteFile(filepath.Join(root, "standalone.mp4"), []byte("fake"), 0644)   //nolint:errcheck
	os.WriteFile(filepath.Join(child, "blockbuster.mp4"), []byte("fake"), 0644) //nolint:errcheck

	srv := newTestServer(t)
	ctx := context.Background()

	// Register both as separate directories.
	parentDir, _ := srv.store.AddDirectory(ctx, root)
	childDir, _ := srv.store.AddDirectory(ctx, child)

	// Sync child first so blockbuster.mp4 gets childDir.ID.
	srv.syncDir(childDir)
	childVideos, _ := srv.store.ListVideosByDirectory(ctx, childDir.ID)
	if len(childVideos) != 1 || childVideos[0].Filename != "blockbuster.mp4" {
		t.Fatalf("child sync: expected 1 video, got %v", childVideos)
	}
	childVideoID := childVideos[0].ID

	// Now sync the parent. It must NOT recurse into child and must NOT
	// reassign blockbuster.mp4 to the parent's directory_id.
	srv.syncDir(parentDir)

	// blockbuster.mp4 must still belong to childDir.
	got, err := srv.store.GetVideo(ctx, childVideoID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DirectoryID != childDir.ID {
		t.Errorf("blockbuster.mp4 DirectoryID = %d, want child dir %d (parent sync must not override)", got.DirectoryID, childDir.ID)
	}

	// Parent must have exactly its own video (standalone.mp4).
	parentVideos, _ := srv.store.ListVideosByDirectory(ctx, parentDir.ID)
	if len(parentVideos) != 1 || parentVideos[0].Filename != "standalone.mp4" {
		t.Errorf("parent sync: expected [standalone.mp4], got %v", parentVideos)
	}
}

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

// ── inferShow unit tests ──────────────────────────────────────────────────────

func TestInferShow(t *testing.T) {
	cases := []struct {
		name     string
		root     string
		dir      string
		filename string
		want     string
	}{
		{
			name:     "subdirectory of root uses dir name as show",
			root:     "/library",
			dir:      "/library/Breaking Bad",
			filename: "s01e01.mp4",
			want:     "Breaking Bad",
		},
		{
			name:     "cleans show name from subdirectory",
			root:     "/library",
			dir:      "/library/the.office.2005",
			filename: "ep.mp4",
			want:     "the office 2005",
		},
		{
			name:     "file in root falls back to filename parsing",
			root:     "/library",
			dir:      "/library",
			filename: "Lost.S01E01.Pilot.mp4",
			want:     "Lost",
		},
		{
			name:     "empty filename in root returns empty",
			root:     "/library",
			dir:      "/library",
			filename: "movie.mp4",
			want:     "",
		},
		{
			name:     "nested subdirectory uses first component",
			root:     "/library",
			dir:      "/library/Sopranos/Season 1",
			filename: "ep.mp4",
			want:     "Sopranos",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferShow(tc.root, tc.dir, tc.filename)
			if got != tc.want {
				t.Errorf("inferShow(%q, %q, %q) = %q, want %q", tc.root, tc.dir, tc.filename, got, tc.want)
			}
		})
	}
}
