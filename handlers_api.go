// handlers_api.go – JSON API for external clients (Roku, mobile apps, etc.).
//
// All endpoints live under /api/... and return application/json.
// Stream URLs (/video/{id}) and thumbnail URLs (/videos/{id}/thumbnail) are
// returned as root-relative paths; callers prepend their server base URL.
//
// Authentication: the same cookie/password middleware that protects the web UI
// also covers /api routes.  For Roku, either run without a password on a
// trusted LAN, or add a dedicated API token via settings (future work).
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maxgarvey/video_manger/store"
)

// ── Shared helpers ────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("api: json encode failed", "err", err)
	}
}

// ── Wire types ────────────────────────────────────────────────────────────────

// apiVideo is the JSON representation of a single video.
type apiVideo struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Show         string  `json:"show,omitempty"`
	Season       int     `json:"season,omitempty"`
	Episode      int     `json:"episode,omitempty"`
	EpisodeTitle string  `json:"episode_title,omitempty"`
	Genre        string  `json:"genre,omitempty"`
	Channel      string  `json:"channel,omitempty"`
	Studio       string  `json:"studio,omitempty"`
	Actors       string  `json:"actors,omitempty"`
	AirDate      string  `json:"air_date,omitempty"`
	Type         string  `json:"type,omitempty"`
	Rating       int     `json:"rating"`
	WatchedAt    string  `json:"watched_at,omitempty"`
	DurationS    float64 `json:"duration_s,omitempty"`
	StreamURL    string  `json:"stream_url"`
	ThumbnailURL string  `json:"thumbnail_url,omitempty"`
}

// apiShow is a summary of a show/series.
type apiShow struct {
	Title        string `json:"title"`
	SeasonCount  int    `json:"season_count"`
	EpisodeCount int    `json:"episode_count"`
	Genre        string `json:"genre,omitempty"`
	Channel      string `json:"channel,omitempty"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// apiSeason summarises one season within a show.
type apiSeason struct {
	Show         string `json:"show"`
	Number       int    `json:"number"`
	EpisodeCount int    `json:"episode_count"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// apiTag is the JSON representation of a tag.
type apiTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// apiWatchedEntry pairs a video with its last resume position.
type apiWatchedEntry struct {
	apiVideo
	PositionS float64 `json:"position_s"`
}

func videoToAPI(v store.Video) apiVideo {
	av := apiVideo{
		ID:           v.ID,
		Title:        v.Title(),
		Show:         v.ShowName,
		Season:       v.SeasonNumber,
		Episode:      v.EpisodeNumber,
		EpisodeTitle: v.EpisodeTitle,
		Genre:        v.Genre,
		Channel:      v.Channel,
		Studio:       v.Studio,
		Actors:       v.Actors,
		AirDate:      v.AirDate,
		Type:         v.VideoType,
		Rating:       v.Rating,
		WatchedAt:    v.WatchedAt,
		DurationS:    v.DurationS,
		StreamURL:    "/video/" + strconv.FormatInt(v.ID, 10),
	}
	if v.ThumbnailPath != "" {
		av.ThumbnailURL = "/videos/" + strconv.FormatInt(v.ID, 10) + "/thumbnail"
	}
	return av
}

// ── /api/videos ───────────────────────────────────────────────────────────────

// GET /api/videos
// Optional query params:
//
//	q=<search term>   full-text search
//	show=<name>       filter by show name
//	type=<TV|Movie…>  filter by video type
//	tag_id=<id>       filter by tag ID
func (s *server) handleAPIListVideos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var (
		videos []store.Video
		err    error
	)
	switch {
	case q.Get("q") != "":
		videos, err = s.store.SearchVideos(r.Context(), q.Get("q"))
	case q.Get("tag_id") != "":
		tagID, _ := strconv.ParseInt(q.Get("tag_id"), 10, 64)
		videos, err = s.store.ListVideosByTag(r.Context(), tagID)
	case q.Get("show") != "":
		videos, err = s.store.ListVideosByShow(r.Context(), q.Get("show"))
	case q.Get("type") != "":
		videos, err = s.store.ListVideosByType(r.Context(), q.Get("type"))
	default:
		videos, err = s.store.ListVideos(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]apiVideo, len(videos))
	for i, v := range videos {
		result[i] = videoToAPI(v)
	}
	writeJSON(w, result)
}

// GET /api/videos/{id}
func (s *server) handleAPIGetVideo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	v, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, videoToAPI(v))
}

// GET /api/random
func (s *server) handleAPIRandom(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.GetRandomVideo(r.Context())
	if err != nil {
		http.Error(w, "no videos", http.StatusNotFound)
		return
	}
	writeJSON(w, videoToAPI(v))
}

// ── /api/shows ────────────────────────────────────────────────────────────────

// GET /api/shows
// Returns all shows that have at least one video with a show name set.
func (s *server) handleAPIListShows(w http.ResponseWriter, r *http.Request) {
	videos, err := s.store.ListVideos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type showAccum struct {
		seasons map[int]struct{}
		count   int
		genre   string
		channel string
		thumb   string
	}
	accum := map[string]*showAccum{}
	var order []string

	for _, v := range videos {
		if v.ShowName == "" {
			continue
		}
		sa, ok := accum[v.ShowName]
		if !ok {
			sa = &showAccum{seasons: map[int]struct{}{}}
			accum[v.ShowName] = sa
			order = append(order, v.ShowName)
		}
		sa.count++
		if v.SeasonNumber > 0 {
			sa.seasons[v.SeasonNumber] = struct{}{}
		}
		if sa.genre == "" && v.Genre != "" {
			sa.genre = v.Genre
		}
		if sa.channel == "" && v.Channel != "" {
			sa.channel = v.Channel
		}
		if sa.thumb == "" && v.ThumbnailPath != "" {
			sa.thumb = "/videos/" + strconv.FormatInt(v.ID, 10) + "/thumbnail"
		}
	}

	result := make([]apiShow, 0, len(order))
	for _, title := range order {
		sa := accum[title]
		result = append(result, apiShow{
			Title:        title,
			SeasonCount:  len(sa.seasons),
			EpisodeCount: sa.count,
			Genre:        sa.genre,
			Channel:      sa.channel,
			ThumbnailURL: sa.thumb,
		})
	}
	writeJSON(w, result)
}

// GET /api/shows/{show}/seasons
func (s *server) handleAPIListSeasons(w http.ResponseWriter, r *http.Request) {
	show, err := url.PathUnescape(chi.URLParam(r, "show"))
	if err != nil || show == "" {
		http.Error(w, "invalid show", http.StatusBadRequest)
		return
	}
	videos, err := s.store.ListVideosByShow(r.Context(), show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type seasonAccum struct {
		count int
		thumb string
	}
	accum := map[int]*seasonAccum{}
	var nums []int

	for _, v := range videos {
		sa, ok := accum[v.SeasonNumber]
		if !ok {
			sa = &seasonAccum{}
			accum[v.SeasonNumber] = sa
			nums = append(nums, v.SeasonNumber)
		}
		sa.count++
		if sa.thumb == "" && v.ThumbnailPath != "" {
			sa.thumb = "/videos/" + strconv.FormatInt(v.ID, 10) + "/thumbnail"
		}
	}

	sort.Ints(nums)
	result := make([]apiSeason, len(nums))
	for i, n := range nums {
		sa := accum[n]
		result[i] = apiSeason{
			Show:         show,
			Number:       n,
			EpisodeCount: sa.count,
			ThumbnailURL: sa.thumb,
		}
	}
	writeJSON(w, result)
}

// GET /api/shows/{show}/seasons/{season}/episodes
func (s *server) handleAPIListEpisodes(w http.ResponseWriter, r *http.Request) {
	show, err := url.PathUnescape(chi.URLParam(r, "show"))
	if err != nil || show == "" {
		http.Error(w, "invalid show", http.StatusBadRequest)
		return
	}
	seasonNum, err := strconv.Atoi(chi.URLParam(r, "season"))
	if err != nil {
		http.Error(w, "invalid season", http.StatusBadRequest)
		return
	}

	videos, err := s.store.ListVideosByShow(r.Context(), show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]apiVideo, 0)
	for _, v := range videos {
		if v.SeasonNumber == seasonNum {
			result = append(result, videoToAPI(v))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Episode < result[j].Episode
	})
	writeJSON(w, result)
}

// ── /api/tags ─────────────────────────────────────────────────────────────────

// GET /api/tags
func (s *server) handleAPIListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]apiTag, len(tags))
	for i, t := range tags {
		result[i] = apiTag{ID: t.ID, Name: t.Name}
	}
	writeJSON(w, result)
}

// GET /api/tags/{id}/videos
func (s *server) handleAPITagVideos(w http.ResponseWriter, r *http.Request) {
	tagID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid tag id", http.StatusBadRequest)
		return
	}
	videos, err := s.store.ListVideosByTag(r.Context(), tagID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]apiVideo, len(videos))
	for i, v := range videos {
		result[i] = videoToAPI(v)
	}
	writeJSON(w, result)
}

// ── /api/recently-watched ─────────────────────────────────────────────────────

// GET /api/recently-watched
// Returns up to 50 videos sorted by most recently watched, each with the last
// resume position in seconds so the Roku channel can offer to continue.
func (s *server) handleAPIRecentlyWatched(w http.ResponseWriter, r *http.Request) {
	history, err := s.store.ListWatchHistory(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type entry struct {
		id        int64
		watchedAt string
		position  float64
	}
	entries := make([]entry, 0, len(history))
	for id, rec := range history {
		entries = append(entries, entry{id, rec.WatchedAt, rec.Position})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].watchedAt > entries[j].watchedAt
	})
	if len(entries) > 50 {
		entries = entries[:50]
	}

	result := make([]apiWatchedEntry, 0, len(entries))
	for _, e := range entries {
		v, err := s.store.GetVideo(r.Context(), e.id)
		if err != nil {
			continue
		}
		result = append(result, apiWatchedEntry{
			apiVideo:  videoToAPI(v),
			PositionS: e.position,
		})
	}
	writeJSON(w, result)
}

// ── Directories ───────────────────────────────────────────────────────────────

type apiDirectory struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

func (s *server) handleAPIDirectories(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]apiDirectory, len(dirs))
	for i, d := range dirs {
		out[i] = apiDirectory{ID: d.ID, Path: d.Path}
	}
	writeJSON(w, out)
}

// ── Folder background images ───────────────────────────────────────────────────

// GET /api/folder-backgrounds — returns {showName: imagePath, ...}
func (s *server) handleGetFolderBackgrounds(w http.ResponseWriter, r *http.Request) {
	pairs, err := s.store.ListSettingsWithPrefix(r.Context(), "folder_bg:")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make(map[string]string, len(pairs))
	for k, v := range pairs {
		show := strings.TrimPrefix(k, "folder_bg:")
		result[show] = v
	}
	writeJSON(w, result)
}

// POST /api/folder-background — saves folder_bg:SHOW → path
func (s *server) handleSetFolderBackground(w http.ResponseWriter, r *http.Request) {
	show := strings.TrimSpace(r.FormValue("show"))
	path := strings.TrimSpace(r.FormValue("path"))
	if show == "" {
		http.Error(w, "show name required", http.StatusBadRequest)
		return
	}
	key := "folder_bg:" + show
	if err := s.store.SaveSettings(r.Context(), map[string]string{key: path}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// GET /api/serve-image?path=... — validates path is under a registered directory then serves it.
func (s *server) handleServeImage(w http.ResponseWriter, r *http.Request) {
	imgPath := r.URL.Query().Get("path")
	if imgPath == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	imgPath = filepath.Clean(imgPath)
	dirs, err := s.store.ListDirectories(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allowed := false
	for _, d := range dirs {
		if strings.HasPrefix(imgPath, filepath.Clean(d.Path)) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "path not under a registered directory", http.StatusForbidden)
		return
	}
	http.ServeFile(w, r, imgPath)
}
