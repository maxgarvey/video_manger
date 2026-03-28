// server.go defines the server struct (which holds all shared state) and the
// routes() method that registers every HTTP route via chi.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	"github.com/maxgarvey/video_manger/store"
)

// ytdlpJob tracks a running yt-dlp download. Lines are sent to ch as
// they are produced; ch is closed when the download finishes. err is set
// (if non-nil) before ch is closed.
type ytdlpJob struct {
	ch      chan string
	err     error
	videoID int64 // set after successful sync; 0 if unknown
}

// convertJob tracks a running ffmpeg conversion. Lines are sent to ch as
// they are produced; ch is closed when the job finishes. err is set (if
// non-nil) and outName is set (on success) before ch is closed.
type convertJob struct {
	ch      chan string
	err     error
	outName string // output filename, set on success
}

// bulkMoveJob tracks a running bulk-move operation. Progress lines are sent
// to ch as each file is processed; ch is closed when the job finishes.
type bulkMoveJob struct {
	ch    chan string
	err   error
	moved int
	fails int
	total int
}

type server struct {
	store         store.Store
	port          string
	mdnsName      string               // e.g. "video-manger.local"
	passwordHash  []byte               // nil means no authentication required
	secureCookies bool                 // set Secure flag on session cookie (requires HTTPS)
	sessions      map[string]time.Time // token → expiry (7-day TTL)
	sessionsMu    sync.RWMutex
	syncingDirs   map[int64]struct{}
	syncingMu     sync.Mutex
	convertSem    chan struct{}        // limits concurrent ffmpeg/yt-dlp processes
	jobs          map[string]*ytdlpJob // active yt-dlp download jobs
	jobsMu        sync.Mutex
	convertJobs   map[string]*convertJob // active ffmpeg convert jobs
	convertJobsMu sync.Mutex
	moveJobs      map[string]*bulkMoveJob // active bulk-move jobs
	moveJobsMu    sync.Mutex
	// Roku cast: one pending video to play, cleared after the Roku polls it.
	castVideoID  int64
	castPostedAt time.Time
	castLastPoll time.Time // updated on every /roku/poll call
	castMu       sync.Mutex
}

// seasonGroup holds videos belonging to a particular season within a show.
type seasonGroup struct {
	Number int
	Videos []store.Video
}

// videoGroup is a view-layer grouping of videos by show/series. Standalone
// videos (no show_name) are grouped by their directory base name.
type videoGroup struct {
	Show    string
	Seasons []seasonGroup
}

// groupVideosByShowSeason groups a flat video slice first by show name (or
// directory name when show is absent), then by season number. Order of videos
// is preserved from the input slice.
func groupVideosByShowSeason(videos []store.Video) []videoGroup {
	var groups []videoGroup
	idx := map[string]int{} // show key → groups index
	for _, v := range videos {
		key := v.ShowName
		if key == "" {
			key = filepath.Base(v.DirectoryPath)
		}
		gi, ok := idx[key]
		if !ok {
			gi = len(groups)
			idx[key] = gi
			groups = append(groups, videoGroup{Show: key})
		}
		// find or create season group
		sn := v.SeasonNumber
		si := -1
		for i, sg := range groups[gi].Seasons {
			if sg.Number == sn {
				si = i
				break
			}
		}
		if si == -1 {
			si = len(groups[gi].Seasons)
			groups[gi].Seasons = append(groups[gi].Seasons, seasonGroup{Number: sn})
		}
		groups[gi].Seasons[si].Videos = append(groups[gi].Seasons[si].Videos, v)
	}
	return groups
}

// authMiddleware redirects unauthenticated requests to /login when a password
// is configured. The /login and /logout routes are always accessible.
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.passwordHash == nil {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/login" || r.URL.Path == "/logout" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("session")
		if err == nil {
			s.sessionsMu.RLock()
			expiry, ok := s.sessions[cookie.Value]
			s.sessionsMu.RUnlock()
			if ok && time.Now().Before(expiry) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	render(w, "login.html", nil)
}

func (s *server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	pw := r.FormValue("password")
	if bcrypt.CompareHashAndPassword(s.passwordHash, []byte(pw)) != nil {
		render(w, "login.html", "Wrong password.")
		return
	}
	// Generate a session token.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "could not generate session", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(raw)
	expiry := time.Now().Add(sessionTTL)
	s.sessionsMu.Lock()
	s.sessions[token] = expiry
	s.sessionsMu.Unlock()
	// Persist session to DB so it survives a server restart.
	if err := s.store.SaveSession(r.Context(), token, expiry); err != nil {
		slog.Warn("persist session failed", "err", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(s.authMiddleware)

	// Static assets (embedded so the binary works from any working directory)
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLoginSubmit)
	r.Get("/logout", s.handleLogout)

	// Video file streaming, thumbnails, and SSE endpoints must NOT be wrapped
	// by Compress.  The gzip middleware buffers output until the response ends,
	// which prevents SSE events from being flushed incrementally to the client.
	// Video Range requests and already-compressed JPEGs also benefit from
	// bypassing gzip.
	r.Get("/video/{id}", s.handleVideoFile)
	r.Get("/videos/{id}/thumbnail", s.handleServeThumbnail)
	r.Get("/videos/{id}/subtitles", s.handleServeSubtitles)
	r.Get("/ytdlp/job/{jobID}/events", s.handleYTDLPJobEvents)
	r.Get("/videos/{id}/convert/events/{jobID}", s.handleConvertEvents)
	r.Get("/videos/bulk-move/{jobID}/events", s.handleBulkMoveEvents)

	// All remaining routes use gzip compression (HTML/JSON responses).
	r.Group(func(r chi.Router) {
		r.Use(middleware.Compress(5))

		r.Get("/", s.handleIndex)
		r.Get("/info", s.handleInfo)

		// Videos
		r.Get("/videos", s.serveVideoList)
		r.Get("/play/{id}", s.handlePlayer)
		r.Put("/videos/{id}/name", s.handleUpdateVideoName)
		r.Get("/videos/{id}/delete-confirm", s.handleVideoDeleteConfirm)
		r.Delete("/videos/{id}", s.handleDeleteVideo)
		r.Delete("/videos/{id}/file", s.handleDeleteVideoAndFile)
		r.Post("/videos/{id}/relocate", s.handleRelocateVideo)

		// Watch history
		r.Post("/videos/{id}/progress", s.handlePostProgress)
		r.Get("/videos/{id}/progress", s.handleGetProgress)
		r.Post("/videos/{id}/watched", s.handleMarkWatched)
		r.Delete("/videos/{id}/progress", s.handleClearProgress)
		r.Post("/videos/{id}/copy-to-library", s.handleCopyToLibrary)
		r.Post("/videos/{id}/move", s.handleMoveVideo)
		r.Post("/videos/bulk-move", s.handleBulkMoveVideos)
		r.Post("/videos/{id}/rename", s.handleRenameVideo)
		r.Post("/import/upload", s.handleImportUpload)

		// Rating
		r.Post("/videos/{id}/rating", s.handleSetRating)
		// Video Type
		r.Post("/videos/{id}/type", s.handleSetVideoType)
		// Color label
		r.Post("/videos/{id}/color", s.handleSetVideoColor)

		// Export / convert
		r.Post("/videos/{id}/export/usb", s.handleExportUSB)
		r.Post("/videos/{id}/convert", s.handleConvertStart)

		// yt-dlp download
		r.Post("/ytdlp/download", s.handleYTDLPDownload)

		// Metadata lookup (TMDB)
		r.Get("/videos/{id}/lookup", s.handleLookupModal)
		r.Post("/videos/{id}/lookup/search", s.handleLookupSearch)
		r.Get("/videos/{id}/lookup/episodes", s.handleLookupEpisodes)
		r.Post("/videos/{id}/lookup/apply", s.handleLookupApply)

		// Quick label
		r.Get("/videos/{id}/quick-label", s.handleQuickLabelModal)
		r.Post("/videos/{id}/quick-label", s.handleQuickLabelSubmit)

		// P2P share
		r.Get("/videos/{id}/share", s.handleSharePanel)

		// File metadata (ffprobe/ffmpeg)
		r.Get("/videos/{id}/metadata", s.handleGetMetadata)
		r.Get("/videos/{id}/metadata/edit", s.handleEditMetadata)
		r.Put("/videos/{id}/metadata", s.handleUpdateMetadata)

		// Standardised descriptive fields (genre, season/episode, actors, studio, channel)
		r.Get("/videos/{id}/fields", s.handleGetVideoFields)
		r.Get("/videos/{id}/fields/edit", s.handleEditVideoFields)
		r.Put("/videos/{id}/fields", s.handleUpdateVideoFields)

		// Tags
		r.Get("/videos/{id}/tags", s.handleVideoTags)
		r.Post("/videos/{id}/tags", s.handleAddVideoTag)
		r.Delete("/videos/{id}/tags/{tagID}", s.handleRemoveVideoTag)
		r.Get("/tags", s.handleListTags)

		// Settings
		r.Get("/settings", s.handleGetSettings)
		r.Post("/settings", s.handleSaveSettings)

		// Thumbnail generation (serving is outside this group — see above)
		r.Post("/videos/{id}/thumbnail", s.handleGenerateThumbnail)

		// Filesystem browser (used by folder picker in sidebar)
		r.Get("/fs", s.handleBrowseFS)

		// Directories
		r.Get("/directories", s.serveDirList)
		r.Get("/directories/options", s.handleDirectoryOptions)
		r.Post("/directories", s.handleAddDirectory)
		r.Post("/directories/create", s.handleCreateDirectory)
		r.Get("/directories/{id}/delete-confirm", s.handleDirectoryDeleteConfirm)
		r.Post("/directories/{id}/sync", s.handleSyncDirectory)
		r.Delete("/directories/{id}", s.handleDeleteDirectory)
		r.Delete("/directories/{id}/files", s.handleDeleteDirectoryAndFiles)
		r.Post("/directories/{id}/subfolder", s.handleCreateSubfolder)

		// Duplicate detection
		r.Get("/duplicates", s.handleListDuplicates)

		// Video trimming (temporal crop)
		r.Post("/videos/{id}/trim", s.handleTrim)

		// Watermark removal (delogo)
		r.Post("/videos/{id}/delogo", s.handleDelogo)

		// Random video ID (for initial tab load)
		r.Get("/random-video", s.handleRandomVideoID)

		// Next unwatched video
		r.Get("/videos/next-unwatched", s.handleNextUnwatched)

		// ── JSON API (Roku / external clients) ──────────────────────────
		r.Get("/api/videos", s.handleAPIListVideos)
		r.Get("/api/videos/{id}", s.handleAPIGetVideo)
		r.Get("/api/random", s.handleAPIRandom)
		r.Get("/api/shows", s.handleAPIListShows)
		r.Get("/api/shows/{show}/seasons", s.handleAPIListSeasons)
		r.Get("/api/shows/{show}/seasons/{season}/episodes", s.handleAPIListEpisodes)
		r.Get("/api/tags", s.handleAPIListTags)
		r.Get("/api/tags/{id}/videos", s.handleAPITagVideos)
		r.Get("/api/recently-watched", s.handleAPIRecentlyWatched)
		r.Get("/api/directories", s.handleAPIDirectories)

		// Folder background images
		r.Get("/api/folder-backgrounds", s.handleGetFolderBackgrounds)
		r.Post("/api/folder-background", s.handleSetFolderBackground)
		r.Get("/api/serve-image", s.handleServeImage)

		// Roku cast — web UI posts a video to play; Roku polls and consumes it.
		r.Post("/roku/cast/{id}", s.handleRokuCast)
		r.Get("/roku/poll", s.handleRokuPoll)
		r.Get("/roku/connected", s.handleRokuConnected)
	})

	return r
}

// startSessionPruner removes expired sessions once per hour.
func (s *server) startSessionPruner(ctx context.Context) {
	ticker := time.NewTicker(sessionPruneEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.sessionsMu.Lock()
			for tok, exp := range s.sessions {
				if now.After(exp) {
					delete(s.sessions, tok)
				}
			}
			s.sessionsMu.Unlock()
			if err := s.store.PruneExpiredSessions(ctx); err != nil {
				slog.Warn("prune expired sessions failed", "err", err)
			}
		}
	}
}
