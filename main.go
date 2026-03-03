// main.go wires together the application: parses CLI flags, opens the SQLite
// database, starts background services (session pruning, mDNS), launches the
// HTTP server, and handles graceful shutdown on SIGTERM/SIGINT.
// It also hosts small shared helpers: render(), parseIDParam(), reltime(), etc.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/grandcat/zeroconf"
	"golang.org/x/crypto/bcrypt"

	"github.com/maxgarvey/video_manger/store"
)

//go:embed templates/*
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"base":    filepath.Base,
	"reltime": reltime,
	"add":     func(a, b int) int { return a + b },
	"mul":     func(a, b int) int { return a * b },
	"ext": func(filename string) string {
		e := filepath.Ext(filename)
		if len(e) > 1 {
			return e[1:] // strip leading dot
		}
		return e
	},
}).ParseFS(templateFS, "templates/*.html"))

// Server tunables – change these to adjust behaviour without recompiling.
const (
	sessionTTL        = 7 * 24 * time.Hour // session cookie lifetime
	sessionPruneEvery = time.Hour           // how often to run the session pruner
	libraryPollEvery  = 60 * time.Second    // how often to re-scan directories
	convertConcurrent = 2                   // max concurrent ffmpeg/yt-dlp processes
)

// reltime formats a SQLite datetime string (UTC, "2006-01-02 15:04:05") as a
// human-readable relative duration: "just now", "5 mins ago", "yesterday", "Jan 2".
func reltime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return s
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hr ago"
		}
		return fmt.Sprintf("%d hrs ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

// parseIDParam extracts and parses the "{id}" URL parameter as int64.
// On error it writes a 400 response and returns false.
func parseIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// render executes the named template, writing a 500 on error.
func render(w http.ResponseWriter, name string, data any) {
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render template failed", "template", name, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	dbPath := flag.String("db", "video_manger.db", "path to SQLite database file")
	dir := flag.String("dir", "", "video directory to register on startup (optional)")
	port := flag.String("port", "8080", "port to listen on")
	password := flag.String("password", "", "optional password to protect the UI (leave empty for no auth)")
	flag.Parse()

	s, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	srv := &server{
		store:       s,
		port:        *port,
		mdnsName:    "video-manger.local",
		sessions:    make(map[string]time.Time),
		syncingDirs: make(map[int64]struct{}),
		convertSem:  make(chan struct{}, convertConcurrent),
		jobs:        make(map[string]*ytdlpJob),
		convertJobs: make(map[string]*convertJob),
	}
	if *password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("hash password: %v", err)
		}
		srv.passwordHash = hash
		slog.Info("password protection enabled")
	}

	// Restore persisted sessions so logins survive a server restart.
	if savedSessions, err := srv.store.LoadSessions(context.Background()); err == nil {
		srv.sessions = savedSessions
	}

	if *dir != "" {
		d, err := srv.store.AddDirectory(context.Background(), *dir)
		if err != nil {
			slog.Warn("could not register startup dir", "path", *dir, "err", err)
		} else {
			srv.syncDir(d)
		}
	}

	portInt, _ := strconv.Atoi(*port) // zero is fine; zeroconf is best-effort
	mdns, err := zeroconf.Register("video-manger", "_http._tcp", "local.", portInt, nil, nil)
	if err != nil {
		slog.Warn("mDNS register failed", "err", err)
	} else {
		defer mdns.Shutdown()
		slog.Info("mDNS registered", "url", "http://video-manger.local:"+*port)
	}

	checkBinaries()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go srv.startLibraryPoller(ctx)
	go srv.startSessionPruner(ctx)

	httpSrv := &http.Server{Addr: ":" + *port, Handler: srv.routes()}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()

	slog.Info("server started", "url", "http://localhost:"+*port)
	for _, addr := range localAddresses(*port) {
		slog.Info("LAN address", "url", addr)
	}

	<-ctx.Done()
	slog.Info("shutting down")
	stop()

	// Give in-flight short requests a moment to finish, then force-close.
	// Long-lived connections (video streams, SSE) would otherwise block for
	// the full timeout, so we close immediately after the grace period.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		httpSrv.Close() //nolint:errcheck
	}
	s.Close() //nolint:errcheck
}
