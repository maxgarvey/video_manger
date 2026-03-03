// handlers.go – package-level constants, template data types, and small
// helpers shared across handlers_videos.go, handlers_directories.go,
// handlers_conversion.go, and handlers_metadata.go.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
)

// ── Handler constants ────────────────────────────────────────────────────────

const (
	// maxUploadBytes caps the request body for file uploads (8 GB).
	maxUploadBytes = 8 << 30
	// multipartMemBytes is the in-memory buffer used when parsing multipart forms.
	multipartMemBytes = 64 << 20
)

// ── Template data types ──────────────────────────────────────────────────────

// videoTagsData is the template model for video_tags.html.
type videoTagsData struct {
	VideoID int64
	Tags    []store.Tag
}

// fileMetaData is the template model for file_metadata.html and
// file_metadata_edit.html. Streams may be nil when rendered from a
// post-write context (saves an extra ffprobe round-trip).
// Warn is set to a non-empty string when a metadata write fails so the
// template can surface a visible warning to the user.
type fileMetaData struct {
	VideoID int64
	Native  metadata.Meta
	Streams []metadata.Stream
	Warn    string
}

// ── Shared helpers ───────────────────────────────────────────────────────────

// localAddresses returns http:// URLs for each non-loopback IPv4 address
// on the machine, using the given port.
func localAddresses(port string) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			result = append(result, "http://"+ip.String()+":"+port)
		}
	}
	return result
}

// videoOrError fetches the video identified by the "{id}" URL parameter.
// On any error it writes an appropriate HTTP response and returns false.
func (s *server) videoOrError(w http.ResponseWriter, r *http.Request) (store.Video, bool) {
	id, ok := parseIDParam(w, r)
	if !ok {
		return store.Video{}, false
	}
	video, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return store.Video{}, false
	}
	return video, true
}

// formPtr reads the named form field and returns a pointer to its value.
// Useful for building metadata.Updates structs where nil means "leave unchanged".
func formPtr(r *http.Request, key string) *string {
	v := r.FormValue(key)
	return &v
}

// strPtr is a tiny helper used by parseYTDLPInfoJSON.
func strPtr(s string) *string { return &s }

// newToken returns a cryptographically random 16-hex-char token string.
// It panics only if the system entropy source is exhausted, which is never
// expected in practice.
func newToken() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(raw)
}

// sseWriter wraps an http.ResponseWriter for Server-Sent Events.
// Create one with newSSEWriter; it validates flusher support and sets headers.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// newSSEWriter prepares w for SSE streaming. Returns (nil, false) and writes
// a 500 if w does not implement http.Flusher.
func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &sseWriter{w: w, f: flusher}, true
}

// Data sends a plain SSE data event. Newlines are stripped from line.
func (sw *sseWriter) Data(line string) {
	safe := strings.ReplaceAll(strings.ReplaceAll(line, "\r", ""), "\n", " ")
	fmt.Fprintf(sw.w, "data: %s\n\n", safe) //nolint:errcheck
	sw.f.Flush()
}

// Event sends a named SSE event. Newlines are stripped from data.
func (sw *sseWriter) Event(event, data string) {
	safe := strings.ReplaceAll(data, "\n", " ")
	fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", event, safe) //nolint:errcheck
	sw.f.Flush()
}

// scheduleJobCleanup closes ch immediately and schedules deleteFunc to run
// after 10 minutes so SSE clients that connect late can still read the result.
// Use it with defer at the start of a background job goroutine:
//
//	defer scheduleJobCleanup(job.ch, func() { ... })
func scheduleJobCleanup(ch chan string, deleteFunc func()) {
	close(ch)
	time.AfterFunc(10*time.Minute, deleteFunc)
}

// findRegisteredDir reports whether path equals or is nested inside any of the
// registered directories. Returns the matching directory's ID when path is an
// exact match, 0 when path is a sub-folder, and underLib=false when path is
// not inside the library at all.
func findRegisteredDir(dirs []store.Directory, path string) (dirID int64, underLib bool) {
	for _, d := range dirs {
		if d.Path == path {
			return d.ID, true
		}
		if strings.HasPrefix(path, d.Path+string(filepath.Separator)) {
			return 0, true
		}
	}
	return 0, false
}

// copyFile copies src to dst using a streaming io.Copy.
// If the write fails, the partial destination file is removed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) //nolint:errcheck
		return err
	}
	return out.Close()
}

// openFreeFile atomically creates a new file in dir using O_CREATE|O_EXCL,
// appending a counter suffix (_2, _3, …) if the base name is already taken.
// Returns the open file handle and the final filename chosen.
func openFreeFile(dir, stem, ext string) (*os.File, string, error) {
	try := func(name string) (*os.File, error) {
		return os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	}
	name := stem + ext
	f, err := try(name)
	if err == nil {
		return f, name, nil
	}
	if !os.IsExist(err) {
		return nil, "", err
	}
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s_%d%s", stem, i, ext)
		f, err := try(name)
		if err == nil {
			return f, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
}
