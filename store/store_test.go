package store_test

import (
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
