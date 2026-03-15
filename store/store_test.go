package store_test

import (
	"context"
	"testing"

	"github.com/maxgarvey/video_manger/store"
)

// --- Video.FilePath ---

func TestFilePath(t *testing.T) {
	v := store.Video{DirectoryPath: "/videos/movies", Filename: "film.mp4"}
	if got := v.FilePath(); got != "/videos/movies/film.mp4" {
		t.Errorf("FilePath() = %q, want /videos/movies/film.mp4", got)
	}
}

func TestFilePath_TrailingSlash(t *testing.T) {
	v := store.Video{DirectoryPath: "/videos/", Filename: "clip.mkv"}
	got := v.FilePath()
	// filepath.Join cleans double slashes
	if got != "/videos/clip.mkv" {
		t.Errorf("FilePath() = %q, want /videos/clip.mkv", got)
	}
}

// --- Video.Title ---

func TestTitle_UsesDisplayName(t *testing.T) {
	v := store.Video{Filename: "raw.mp4", DisplayName: "My Movie"}
	if got := v.Title(); got != "My Movie" {
		t.Errorf("Title() = %q, want My Movie", got)
	}
}

func TestTitle_FallsBackToFilename(t *testing.T) {
	v := store.Video{Filename: "raw.mp4"}
	if got := v.Title(); got != "raw.mp4" {
		t.Errorf("Title() = %q, want raw.mp4", got)
	}
}

// --- Video.HasFields ---

func TestHasFields_Empty(t *testing.T) {
	if (store.Video{}).HasFields() {
		t.Error("empty Video.HasFields() should be false")
	}
}

func TestHasFields_Genre(t *testing.T) {
	if !(store.Video{Genre: "Drama"}).HasFields() {
		t.Error("Video{Genre}.HasFields() should be true")
	}
}

func TestHasFields_SeasonNumber(t *testing.T) {
	if !(store.Video{SeasonNumber: 1}).HasFields() {
		t.Error("Video{SeasonNumber:1}.HasFields() should be true")
	}
}

func TestHasFields_EpisodeNumber(t *testing.T) {
	if !(store.Video{EpisodeNumber: 3}).HasFields() {
		t.Error("Video{EpisodeNumber:3}.HasFields() should be true")
	}
}

func TestHasFields_EpisodeTitle(t *testing.T) {
	if !(store.Video{EpisodeTitle: "Pilot"}).HasFields() {
		t.Error("Video{EpisodeTitle}.HasFields() should be true")
	}
}

func TestHasFields_Actors(t *testing.T) {
	if !(store.Video{Actors: "Tom Hanks"}).HasFields() {
		t.Error("Video{Actors}.HasFields() should be true")
	}
}

func TestHasFields_Studio(t *testing.T) {
	if !(store.Video{Studio: "Universal"}).HasFields() {
		t.Error("Video{Studio}.HasFields() should be true")
	}
}

func TestHasFields_Channel(t *testing.T) {
	if !(store.Video{Channel: "HBO"}).HasFields() {
		t.Error("Video{Channel}.HasFields() should be true")
	}
}

// --- IsValidVideoType ---

func TestIsValidVideoType_EmptyIsValid(t *testing.T) {
	if !store.IsValidVideoType("") {
		t.Error("empty string should be valid (means unset)")
	}
}

func TestIsValidVideoType_KnownTypes(t *testing.T) {
	for _, typ := range []string{"TV", "Movie", "Concert", "Vlog", "Blog", "YouTube"} {
		if !store.IsValidVideoType(typ) {
			t.Errorf("IsValidVideoType(%q) = false, want true", typ)
		}
	}
}

func TestIsValidVideoType_UnknownType(t *testing.T) {
	if store.IsValidVideoType("Podcast") {
		t.Error("IsValidVideoType(Podcast) = true, want false")
	}
}

// --- ValidVideoTypes ---

func TestValidVideoTypes_ContainsAllKnown(t *testing.T) {
	types := store.ValidVideoTypes()
	seen := make(map[string]bool, len(types))
	for _, t := range types {
		seen[t] = true
	}
	for _, want := range []string{"TV", "Movie", "Concert", "Vlog", "Blog", "YouTube"} {
		if !seen[want] {
			t.Errorf("ValidVideoTypes missing %q", want)
		}
	}
}

func TestValidVideoTypes_NoDuplicates(t *testing.T) {
	types := store.ValidVideoTypes()
	seen := make(map[string]int)
	for _, typ := range types {
		seen[typ]++
	}
	for typ, count := range seen {
		if count > 1 {
			t.Errorf("ValidVideoTypes: %q appears %d times", typ, count)
		}
	}
}

// --- 15. UpdateVideoFields with AirDate ---

func TestUpdateVideoFields_AirDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dir, err := s.AddDirectory(ctx, "/videos")
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	v, err := s.UpsertVideo(ctx, dir.ID, dir.Path, "film.mp4")
	if err != nil {
		t.Fatalf("UpsertVideo: %v", err)
	}

	fields := store.VideoFields{
		Genre:   "Drama",
		AirDate: "2023-04-15",
	}
	if err := s.UpdateVideoFields(ctx, v.ID, fields); err != nil {
		t.Fatalf("UpdateVideoFields: %v", err)
	}

	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.AirDate != "2023-04-15" {
		t.Errorf("AirDate = %q, want 2023-04-15", got.AirDate)
	}
	if got.Genre != "Drama" {
		t.Errorf("Genre = %q, want Drama", got.Genre)
	}
}

// --- 16. UpdateVideoFields clears air_date when set to empty string ---

func TestUpdateVideoFields_ClearsAirDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dir, err := s.AddDirectory(ctx, "/videos")
	if err != nil {
		t.Fatalf("AddDirectory: %v", err)
	}
	v, err := s.UpsertVideo(ctx, dir.ID, dir.Path, "film.mp4")
	if err != nil {
		t.Fatalf("UpsertVideo: %v", err)
	}

	// First set a value.
	if err := s.UpdateVideoFields(ctx, v.ID, store.VideoFields{AirDate: "2023-04-15"}); err != nil {
		t.Fatalf("UpdateVideoFields (set): %v", err)
	}

	// Now clear it by passing an empty string.
	if err := s.UpdateVideoFields(ctx, v.ID, store.VideoFields{AirDate: ""}); err != nil {
		t.Fatalf("UpdateVideoFields (clear): %v", err)
	}

	got, err := s.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.AirDate != "" {
		t.Errorf("AirDate = %q after clear, want empty string", got.AirDate)
	}
}

// --- Video.HasFields with AirDate ---

func TestHasFields_AirDate(t *testing.T) {
	if !(store.Video{AirDate: "2023-04-15"}).HasFields() {
		t.Error("Video{AirDate}.HasFields() should be true")
	}
}

// --- IsValidColorLabel ---

func TestIsValidColorLabel_EmptyIsValid(t *testing.T) {
	if !store.IsValidColorLabel("") {
		t.Error("empty string should be valid (means unset)")
	}
}

func TestIsValidColorLabel_KnownColors(t *testing.T) {
	for _, c := range []string{"red", "orange", "yellow", "green", "blue", "purple"} {
		if !store.IsValidColorLabel(c) {
			t.Errorf("IsValidColorLabel(%q) = false, want true", c)
		}
	}
}

func TestIsValidColorLabel_UnknownColor(t *testing.T) {
	if store.IsValidColorLabel("pink") {
		t.Error("IsValidColorLabel(pink) = true, want false")
	}
}

func TestIsValidColorLabel_CaseSensitive(t *testing.T) {
	if store.IsValidColorLabel("Red") {
		t.Error("IsValidColorLabel(Red) = true, want false (case-sensitive)")
	}
}
