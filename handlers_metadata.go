// handlers_metadata.go – file metadata (ffprobe/ffmpeg), video fields,
// tags list, TMDB lookup, and settings handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

// ── TMDB client ───────────────────────────────────────────────────────────────

const (
	tmdbTimeout     = 10 * time.Second
	tmdbResultLimit = 5
)

var tmdbClient = &http.Client{Timeout: tmdbTimeout}

// tmdbEpisode holds a single episode from the TMDB /tv/{id}/season/{n} endpoint.
type tmdbEpisode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
	Overview      string `json:"overview"`
}

// lookupHints holds season/episode numbers inferred from a video's existing data.
type lookupHints struct {
	Season  int
	Episode int
}

// hintsForVideo extracts season/episode hints from DB fields first, then
// ffprobe metadata, then filename pattern (e.g. S02E05).
func hintsForVideo(v store.Video, filePath string) lookupHints {
	if v.SeasonNumber > 0 || v.EpisodeNumber > 0 {
		return lookupHints{Season: v.SeasonNumber, Episode: v.EpisodeNumber}
	}
	m, err := metadata.Read(filePath)
	if err == nil {
		if sn, _ := strconv.Atoi(m.SeasonNum); sn > 0 {
			ep, _ := strconv.Atoi(m.EpisodeNum)
			return lookupHints{Season: sn, Episode: ep}
		}
		if m.EpisodeID != "" {
			if s, e := parseEpisodeID(m.EpisodeID); s > 0 {
				return lookupHints{Season: s, Episode: e}
			}
		}
	}
	return parseFilenameHints(v.Filename)
}

// parseEpisodeID parses "S02E05" or "s02e05" into season=2, episode=5.
func parseEpisodeID(id string) (int, int) {
	id = strings.ToUpper(id)
	si := strings.Index(id, "S")
	if si < 0 {
		return 0, 0
	}
	rest := id[si+1:]
	ei := strings.Index(rest, "E")
	if ei < 0 {
		return 0, 0
	}
	sn, err1 := strconv.Atoi(rest[:ei])
	en, err2 := strconv.Atoi(rest[ei+1:])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return sn, en
}

// parseFilenameHints scans a filename for SxxExx patterns, e.g. "Show.S02E05.mkv".
func parseFilenameHints(filename string) lookupHints {
	if i := strings.LastIndex(filename, "."); i > 0 {
		filename = filename[:i]
	}
	upper := strings.ToUpper(filename)
	for i := 0; i+3 < len(upper); i++ {
		if upper[i] != 'S' {
			continue
		}
		j := i + 1
		for j < len(upper) && upper[j] >= '0' && upper[j] <= '9' {
			j++
		}
		if j == i+1 || j >= len(upper) || upper[j] != 'E' {
			continue
		}
		sn, _ := strconv.Atoi(upper[i+1 : j])
		k := j + 1
		for k < len(upper) && upper[k] >= '0' && upper[k] <= '9' {
			k++
		}
		if k == j+1 {
			continue
		}
		en, _ := strconv.Atoi(upper[j+1 : k])
		if sn > 0 {
			return lookupHints{Season: sn, Episode: en}
		}
	}
	return lookupHints{Season: 1}
}

// tmdbResult holds a single search result from the TMDB /search/multi endpoint.
type tmdbResult struct {
	ID           int    `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"` // movies
	Name         string `json:"name"`  // TV shows
	Overview     string `json:"overview"`
	ReleaseDate  string `json:"release_date"`   // movies
	FirstAirDate string `json:"first_air_date"` // TV shows
}

// DisplayTitle returns the human-readable title regardless of media type.
func (r tmdbResult) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

// Year returns the 4-digit release year, or an empty string if unknown.
func (r tmdbResult) Year() string {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAirDate
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

// tmdbGet performs an authenticated GET against the TMDB v3 API and
// JSON-decodes the response into v.
// Supports both v3 API keys (32-char hex, passed as ?api_key=) and
// v4 Read Access Tokens (JWT starting with "eyJ", passed as Bearer).
func tmdbGet(apiKey, path string, v any) error {
	var rawURL string
	if strings.HasPrefix(apiKey, "eyJ") {
		rawURL = "https://api.themoviedb.org/3" + path
	} else {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		rawURL = "https://api.themoviedb.org/3" + path + sep + "api_key=" + url.QueryEscape(apiKey)
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	if strings.HasPrefix(apiKey, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB API error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// ── File metadata (ffprobe / ffmpeg) ─────────────────────────────────────────

func (s *server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("read metadata failed", "path", video.FilePath(), "err", err)
	}
	streams, err := metadata.ReadStreams(video.FilePath())
	if err != nil {
		slog.Warn("read streams failed", "path", video.FilePath(), "err", err)
	}
	render(w, "file_metadata.html", fileMetaData{VideoID: video.ID, Native: native, Streams: streams})
}

func (s *server) handleEditMetadata(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("read metadata failed", "path", video.FilePath(), "err", err)
	}
	render(w, "file_metadata_edit.html", fileMetaData{VideoID: video.ID, Native: native})
}

func (s *server) handleUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	u := metadata.Updates{
		Title:       formPtr(r, "title"),
		Description: formPtr(r, "description"),
		Genre:       formPtr(r, "genre"),
		Date:        formPtr(r, "date"),
		Comment:     formPtr(r, "comment"),
		Show:        formPtr(r, "show"),
		Network:     formPtr(r, "network"),
		EpisodeID:   formPtr(r, "episode_id"),
		SeasonNum:   formPtr(r, "season_number"),
		EpisodeNum:  formPtr(r, "episode_sort"),
	}
	var warn string
	if err := metadata.Write(video.FilePath(), u); err != nil {
		slog.Warn("write metadata failed", "path", video.FilePath(), "err", err)
		warn = "Metadata write failed: " + err.Error()
	}
	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("read metadata after write failed", "path", video.FilePath(), "err", err)
	}
	render(w, "file_metadata.html", fileMetaData{VideoID: video.ID, Native: native, Warn: warn})
}

// ── Video fields ─────────────────────────────────────────────────────────────

func (s *server) handleGetVideoFields(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	render(w, "video_fields.html", video)
}

func (s *server) handleEditVideoFields(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	render(w, "video_fields_edit.html", video)
}

func (s *server) handleUpdateVideoFields(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	seasonNum, _ := strconv.Atoi(r.FormValue("season_number"))
	episodeNum, _ := strconv.Atoi(r.FormValue("episode_number"))
	f := store.VideoFields{
		Genre:         r.FormValue("genre"),
		SeasonNumber:  seasonNum,
		EpisodeNumber: episodeNum,
		EpisodeTitle:  r.FormValue("episode_title"),
		Actors:        r.FormValue("actors"),
		Studio:        r.FormValue("studio"),
		Channel:       r.FormValue("channel"),
	}
	if err := s.store.UpdateVideoFields(r.Context(), id, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"videoLabelled":{"id":%d}}`, id))
	render(w, "video_fields.html", video)
}

// ── Tags list ─────────────────────────────────────────────────────────────────

func (s *server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rand.Shuffle(len(tags), func(i, j int) { tags[i], tags[j] = tags[j], tags[i] })
	render(w, "tags.html", tags)
}

// ── TMDB lookup ───────────────────────────────────────────────────────────────

// requireTMDBKey retrieves the configured TMDB API key. If the key is not set
// it writes a 400 error and returns ("", false).
func (s *server) requireTMDBKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	key = strings.TrimSpace(key)
	if key == "" {
		http.Error(w, "TMDB key not configured", http.StatusBadRequest)
		return "", false
	}
	return key, true
}

// fetchMovieMetadata fetches title, overview, release date, and first genre
// for a TMDB movie ID.
func fetchMovieMetadata(apiKey, tmdbID string) (metadata.Updates, error) {
	var m struct {
		Title       string                  `json:"title"`
		Overview    string                  `json:"overview"`
		ReleaseDate string                  `json:"release_date"`
		Genres      []struct{ Name string } `json:"genres"`
	}
	if err := tmdbGet(apiKey, "/movie/"+tmdbID, &m); err != nil {
		return metadata.Updates{}, err
	}
	genre := ""
	if len(m.Genres) > 0 {
		genre = m.Genres[0].Name
	}
	return metadata.Updates{
		Title:       strPtr(m.Title),
		Description: strPtr(m.Overview),
		Genre:       strPtr(genre),
		Date:        strPtr(m.ReleaseDate),
	}, nil
}

// fetchTVMetadata fetches show + episode details for a TMDB TV series ID.
func fetchTVMetadata(apiKey, tmdbID string, season, episode int) (metadata.Updates, error) {
	var show struct {
		Name     string                  `json:"name"`
		Networks []struct{ Name string } `json:"networks"`
		Genres   []struct{ Name string } `json:"genres"`
	}
	if err := tmdbGet(apiKey, "/tv/"+tmdbID, &show); err != nil {
		return metadata.Updates{}, err
	}
	var ep struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
		AirDate  string `json:"air_date"`
	}
	epPath := fmt.Sprintf("/tv/%s/season/%d/episode/%d", tmdbID, season, episode)
	if err := tmdbGet(apiKey, epPath, &ep); err != nil {
		slog.Warn("TMDB episode fetch failed", "err", err)
	}
	genre, network := "", ""
	if len(show.Genres) > 0 {
		genre = show.Genres[0].Name
	}
	if len(show.Networks) > 0 {
		network = show.Networks[0].Name
	}
	sNum := strconv.Itoa(season)
	eNum := strconv.Itoa(episode)
	return metadata.Updates{
		Title:       strPtr(ep.Name),
		Description: strPtr(ep.Overview),
		Genre:       strPtr(genre),
		Date:        strPtr(ep.AirDate),
		Show:        strPtr(show.Name),
		Network:     strPtr(network),
		SeasonNum:   &sNum,
		EpisodeNum:  &eNum,
	}, nil
}

// applyTMDBSystemTags persists TMDB metadata as system tags in the DB.
func (s *server) applyTMDBSystemTags(ctx context.Context, videoID int64, mediaType string, u metadata.Updates, season int) {
	set := func(ns, val string) {
		if val == "" {
			return
		}
		if err := s.store.SetExclusiveSystemTag(ctx, videoID, ns, val); err != nil {
			slog.Warn("TMDB apply: set tag failed", "ns", ns, "err", err)
		}
	}
	if u.Genre != nil {
		set("genre", *u.Genre)
	}
	if mediaType == "tv" {
		if u.Show != nil {
			set("show", *u.Show)
		}
		if u.Network != nil {
			set("channel", *u.Network)
		}
		if season > 0 {
			set("season", strconv.Itoa(season))
		}
	}
}

func (s *server) handleLookupModal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	apiKey, err := s.store.GetSetting(r.Context(), "tmdb_api_key")
	if err != nil {
		slog.Warn("handleLookupModal: GetSetting tmdb_api_key failed", "err", err)
	}
	render(w, "lookup_modal.html", struct {
		VideoID int64
		HasKey  bool
	}{id, strings.TrimSpace(apiKey) != ""})
}

func (s *server) handleLookupSearch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	apiKey, ok := s.requireTMDBKey(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.FormValue("q"))
	if q == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}

	var result struct {
		Results []tmdbResult `json:"results"`
	}
	if err := tmdbGet(apiKey, "/search/multi?query="+url.QueryEscape(q)+"&page=1", &result); err != nil {
		slog.Warn("TMDB search failed", "err", err)
		http.Error(w, "TMDB search failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Keep only movie/tv results, up to tmdbResultLimit.
	var filtered []tmdbResult
	for _, res := range result.Results {
		if res.MediaType != "movie" && res.MediaType != "tv" {
			continue
		}
		filtered = append(filtered, res)
		if len(filtered) >= tmdbResultLimit {
			break
		}
	}

	var hints lookupHints
	if video, err := s.store.GetVideo(r.Context(), id); err == nil {
		hints = hintsForVideo(video, video.FilePath())
	}

	render(w, "lookup_results.html", struct {
		VideoID     int64
		Results     []tmdbResult
		HintSeason  int
		HintEpisode int
	}{id, filtered, hints.Season, hints.Episode})
}

func (s *server) handleLookupApply(w http.ResponseWriter, r *http.Request) {
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	apiKey, ok := s.requireTMDBKey(w, r)
	if !ok {
		return
	}

	mediaType := r.FormValue("media_type")
	tmdbID := r.FormValue("tmdb_id")
	season, _ := strconv.Atoi(r.FormValue("season"))
	episode, _ := strconv.Atoi(r.FormValue("episode"))

	var (
		u   metadata.Updates
		err error
	)
	switch mediaType {
	case "movie":
		u, err = fetchMovieMetadata(apiKey, tmdbID)
	case "tv":
		u, err = fetchTVMetadata(apiKey, tmdbID, season, episode)
	default:
		http.Error(w, "invalid media_type", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "TMDB fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	var warn string
	if err := metadata.Write(video.FilePath(), u); err != nil {
		slog.Warn("TMDB apply: write failed", "path", video.FilePath(), "err", err)
		warn = "Metadata write failed: " + err.Error()
	}

	s.applyTMDBSystemTags(r.Context(), video.ID, mediaType, u, season)

	native, err := metadata.Read(video.FilePath())
	if err != nil {
		slog.Warn("TMDB apply: read failed", "path", video.FilePath(), "err", err)
	}
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"videoLabelled":{"id":%d}}`, video.ID))
	render(w, "file_metadata.html", fileMetaData{VideoID: video.ID, Native: native, Warn: warn})
}

func (s *server) handleLookupEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	apiKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<p style="font-size:0.82rem;color:#f87">TMDB API key not configured.</p>`)
		return
	}
	tmdbID := r.FormValue("tmdb_id")
	season, _ := strconv.Atoi(r.FormValue("season"))
	if season < 1 {
		season = 1
	}
	var seasonData struct {
		Episodes []tmdbEpisode `json:"episodes"`
	}
	epPath := fmt.Sprintf("/tv/%s/season/%d", tmdbID, season)
	if err := tmdbGet(apiKey, epPath, &seasonData); err != nil {
		slog.Warn("TMDB fetch season failed", "err", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<p style="font-size:0.82rem;color:#f87">Could not load episodes: %s</p>`, html.EscapeString(err.Error()))
		return
	}
	render(w, "lookup_episodes.html", struct {
		VideoID  int64
		TmdbID   string
		Season   int
		Episodes []tmdbEpisode
	}{id, tmdbID, season, seasonData.Episodes})
}

// ── Settings ─────────────────────────────────────────────────────────────────

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	autoplay, _ := s.store.GetSetting(r.Context(), "autoplay_random")
	videoSort, _ := s.store.GetSetting(r.Context(), "video_sort")
	tmdbKey, _ := s.store.GetSetting(r.Context(), "tmdb_api_key")
	libraryPath, _ := s.store.GetSetting(r.Context(), "library_path")
	nextFromSearch, _ := s.store.GetSetting(r.Context(), "next_from_search")
	rokuEnabled, _ := s.store.GetSetting(r.Context(), "roku_enabled")
	render(w, "settings.html", struct {
		AutoplayRandom bool
		VideoSort      string
		HasTMDBKey     bool
		LibraryPath    string
		NextFromSearch bool
		RokuEnabled    bool
	}{
		AutoplayRandom: autoplay == "true",
		VideoSort:      videoSort,
		HasTMDBKey:     strings.TrimSpace(tmdbKey) != "",
		LibraryPath:    strings.TrimSpace(libraryPath),
		NextFromSearch: nextFromSearch == "true",
		RokuEnabled:    rokuEnabled != "false",
	})
}

func (s *server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	autoplay := "false"
	if r.FormValue("autoplay_random") == "on" {
		autoplay = "true"
	}
	nextFromSearch := "false"
	if r.FormValue("next_from_search") == "on" {
		nextFromSearch = "true"
	}
	rokuEnabled := "false"
	if r.FormValue("roku_enabled") == "on" {
		rokuEnabled = "true"
	}
	pairs := map[string]string{
		"autoplay_random":  autoplay,
		"video_sort":       r.FormValue("video_sort"),
		"library_path":     strings.TrimSpace(r.FormValue("library_path")),
		"next_from_search": nextFromSearch,
		"roku_enabled":     rokuEnabled,
	}
	// Only overwrite the TMDB key if a new value was provided.
	if key := strings.TrimSpace(r.FormValue("tmdb_api_key")); key != "" {
		pairs["tmdb_api_key"] = key
	}
	if err := s.store.SaveSettings(r.Context(), pairs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.handleGetSettings(w, r)
}
