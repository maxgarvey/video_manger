package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	ch  chan string
	err error
}

// convertJob tracks a running ffmpeg conversion. Lines are sent to ch as
// they are produced; ch is closed when the job finishes. err is set (if
// non-nil) and outName is set (on success) before ch is closed.
type convertJob struct {
	ch      chan string
	err     error
	outName string // output filename, set on success
}

type server struct {
	store        store.Store
	port         string
	mdnsName     string // e.g. "video-manger.local"
	passwordHash []byte // nil means no authentication required
	sessions     map[string]time.Time // token → expiry (7-day TTL)
	sessionsMu   sync.RWMutex
	syncingDirs  map[int64]struct{}
	syncingMu    sync.Mutex
	convertSem    chan struct{}          // limits concurrent ffmpeg/yt-dlp processes
	jobs          map[string]*ytdlpJob  // active yt-dlp download jobs
	jobsMu        sync.Mutex
	convertJobs   map[string]*convertJob // active ffmpeg convert jobs
	convertJobsMu sync.Mutex
}

// videoGroup is a view-layer grouping of videos sharing the same directory.
type videoGroup struct {
	Label  string // last path component of DirectoryPath
	Videos []store.Video
}

// groupVideosByDir groups a flat video slice by DirectoryPath, preserving
// the order videos appear in the input.
func groupVideosByDir(videos []store.Video) []videoGroup {
	var groups []videoGroup
	idx := map[string]int{} // dirPath → slice index
	for _, v := range videos {
		p := v.DirectoryPath
		if i, ok := idx[p]; ok {
			groups[i].Videos = append(groups[i].Videos, v)
		} else {
			idx[p] = len(groups)
			groups = append(groups, videoGroup{
				Label:  filepath.Base(p),
				Videos: []store.Video{v},
			})
		}
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
	r.Use(middleware.Compress(5))
	r.Use(s.authMiddleware)

	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLoginSubmit)
	r.Get("/logout", s.handleLogout)

	r.Get("/", s.handleIndex)
	r.Get("/info", s.handleInfo)

	// Videos
	r.Get("/videos", s.serveVideoList)
	r.Get("/play/{id}", s.handlePlayer)
	r.Get("/video/{id}", s.handleVideoFile)
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
	r.Post("/import/upload", s.handleImportUpload)

	// Rating
	r.Post("/videos/{id}/rating", s.handleSetRating)

	// Export / convert
	r.Post("/videos/{id}/export/usb", s.handleExportUSB)
	r.Post("/videos/{id}/convert", s.handleConvertStart)
	r.Get("/videos/{id}/convert/events/{jobID}", s.handleConvertEvents)

	// yt-dlp download
	r.Post("/ytdlp/download", s.handleYTDLPDownload)
	r.Get("/ytdlp/job/{jobID}/events", s.handleYTDLPJobEvents)

	// Metadata lookup (TMDB)
	r.Get("/videos/{id}/lookup", s.handleLookupModal)
	r.Post("/videos/{id}/lookup/search", s.handleLookupSearch)
	r.Post("/videos/{id}/lookup/apply", s.handleLookupApply)

	// P2P share
	r.Get("/videos/{id}/share", s.handleSharePanel)

	// File metadata (ffprobe/ffmpeg)
	r.Get("/videos/{id}/metadata", s.handleGetMetadata)
	r.Get("/videos/{id}/metadata/edit", s.handleEditMetadata)
	r.Put("/videos/{id}/metadata", s.handleUpdateMetadata)

	// Tags
	r.Get("/videos/{id}/tags", s.handleVideoTags)
	r.Post("/videos/{id}/tags", s.handleAddVideoTag)
	r.Delete("/videos/{id}/tags/{tagID}", s.handleRemoveVideoTag)
	r.Get("/tags", s.handleListTags)

	// Settings
	r.Get("/settings", s.handleGetSettings)
	r.Post("/settings", s.handleSaveSettings)

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

	// Duplicate detection
	r.Get("/duplicates", s.handleListDuplicates)

	// Video trimming (temporal crop)
	r.Get("/videos/{id}/trim", s.handleTrimPanel)
	r.Post("/videos/{id}/trim", s.handleTrim)

	// Random video ID (for initial tab load)
	r.Get("/random-video", s.handleRandomVideoID)

	// Next unwatched video
	r.Get("/videos/next-unwatched", s.handleNextUnwatched)

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
