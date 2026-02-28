package metadata

import "testing"

func TestParseFFProbeOutput(t *testing.T) {
	data := []byte(`{
		"format": {
			"tags": {
				"title":       "My Movie",
				"genre":       "Action",
				"keywords":    "summer,vacation,family",
				"description": "A great trip",
				"artist":      "John Doe",
				"date":        "2023"
			}
		}
	}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Title != "My Movie" {
		t.Errorf("Title = %q, want My Movie", m.Title)
	}
	if m.Genre != "Action" {
		t.Errorf("Genre = %q, want Action", m.Genre)
	}
	if m.Description != "A great trip" {
		t.Errorf("Description = %q, want 'A great trip'", m.Description)
	}
	if m.Artist != "John Doe" {
		t.Errorf("Artist = %q, want 'John Doe'", m.Artist)
	}
	if m.Date != "2023" {
		t.Errorf("Date = %q, want 2023", m.Date)
	}
	if len(m.Keywords) != 3 {
		t.Fatalf("expected 3 keywords, got %v", m.Keywords)
	}
	for i, want := range []string{"summer", "vacation", "family"} {
		if m.Keywords[i] != want {
			t.Errorf("Keywords[%d] = %q, want %q", i, m.Keywords[i], want)
		}
	}
}

func TestParseFFProbeOutput_SemicolonKeywords(t *testing.T) {
	data := []byte(`{"format": {"tags": {"keywords": "tag1; tag2; tag3"}}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Keywords) != 3 {
		t.Errorf("expected 3 keywords, got %v", m.Keywords)
	}
}

func TestParseFFProbeOutput_Empty(t *testing.T) {
	data := []byte(`{"format": {}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.HasData() {
		t.Errorf("expected empty Meta, got %+v", m)
	}
}

func TestParseFFProbeOutput_FallbackFields(t *testing.T) {
	// artist falls back to album_artist, date falls back to year
	data := []byte(`{"format": {"tags": {"album_artist": "Studio", "year": "2020"}}}`)
	m, err := parseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Artist != "Studio" {
		t.Errorf("Artist = %q, want Studio", m.Artist)
	}
	if m.Date != "2020" {
		t.Errorf("Date = %q, want 2020", m.Date)
	}
}

func TestHasData(t *testing.T) {
	if (Meta{}).HasData() {
		t.Error("empty Meta.HasData() should be false")
	}
	if !(Meta{Title: "x"}).HasData() {
		t.Error("Meta{Title}.HasData() should be true")
	}
	if !(Meta{Keywords: []string{"a"}}).HasData() {
		t.Error("Meta{Keywords}.HasData() should be true")
	}
}
