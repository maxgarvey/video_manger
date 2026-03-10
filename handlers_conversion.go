// handlers_conversion.go – ffmpeg-based conversion, USB export, and trim handlers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/maxgarvey/video_manger/metadata"
	"github.com/maxgarvey/video_manger/store"
	"github.com/maxgarvey/video_manger/transcode"
)

// ── Convert ───────────────────────────────────────────────────────────────────

func (s *server) handleConvertStart(w http.ResponseWriter, r *http.Request) {
	// Validate inputs first so bad requests get proper 4xx responses even
	// when ffmpeg is not installed on the current system.
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}

	formatKey := r.FormValue("format")
	quality := r.FormValue("quality")

	f, fok := transcode.Formats[formatKey]
	if !fok {
		http.Error(w, "unknown format", http.StatusBadRequest)
		return
	}

	src := video.FilePath()
	dir := filepath.Dir(src)
	stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))

	// Reject if the output name would collide with the source (e.g. mkv→mkv).
	if stem+f.Ext == filepath.Base(src) {
		http.Error(w, "output would overwrite source — choose a different format", http.StatusBadRequest)
		return
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		http.Error(w, "ffmpeg is not installed — conversion is unavailable", http.StatusServiceUnavailable)
		return
	}

	outName := freeOutputName(dir, stem, "", f.Ext)
	dst := filepath.Join(dir, outName)

	jobID := newToken()
	// 65536 lines: ffmpeg emits roughly one progress line per frame; a 2-hour
	// film at 24 fps produces ~170 000 lines.  When the buffer is full the
	// non-blocking send silently drops lines so the conversion still completes,
	// but the SSE client may see gaps.  65536 covers most single-pass converts
	// at no more than ~6 MB of heap (a few hundred bytes per string).
	job := &convertJob{ch: make(chan string, 65536)}
	s.convertJobsMu.Lock()
	s.convertJobs[jobID] = job
	s.convertJobsMu.Unlock()

	dirID := video.DirectoryID
	go func() {
		defer scheduleJobCleanup(job.ch, func() {
			s.convertJobsMu.Lock()
			delete(s.convertJobs, jobID)
			s.convertJobsMu.Unlock()
		})

		send := func(line string) {
			select {
			case job.ch <- line:
			default:
			}
		}

		send("[queue] Waiting for convert slot…")
		s.convertSem <- struct{}{}
		defer func() { <-s.convertSem }()

		totalSecs := metadata.ReadDuration(src)
		err := transcode.ConvertProgress(context.Background(), src, dst, f, quality, totalSecs, send)
		if err != nil {
			job.err = err
			if rmErr := os.Remove(dst); rmErr != nil && !os.IsNotExist(rmErr) {
				slog.Warn("convert: remove failed output", "path", dst, "err", rmErr)
			}
		} else {
			job.outName = outName
			if d, err2 := s.store.GetDirectory(context.Background(), dirID); err2 == nil {
				s.startSyncDir(d)
			}
		}
	}()

	render(w, "convert_progress.html", struct {
		JobID   string
		VideoID int64
		OutName string
	}{jobID, video.ID, outName})
}

// handleConvertEvents streams ffmpeg conversion progress for a background job
// as Server-Sent Events. Sends a "done" event with the output filename on
// success, or an "error" event with the error message on failure.
func (s *server) handleConvertEvents(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	s.convertJobsMu.Lock()
	job, ok := s.convertJobs[jobID]
	s.convertJobsMu.Unlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		return
	}

	ctx := r.Context()
loop:
	for {
		select {
		case line, open := <-job.ch:
			if !open {
				break loop
			}
			sse.Data(line)
		case <-ctx.Done():
			return
		}
	}

	if job.err != nil {
		sse.Event("error", job.err.Error())
	} else {
		sse.Event("done", job.outName)
	}
}

// ── USB export ────────────────────────────────────────────────────────────────

// handleExportUSB re-encodes the video as H.264+AAC MP4 optimised for USB
// playback. The output is written to the same directory as the source with a
// "_usb" suffix. The handler blocks until the transcode completes.
func (s *server) handleExportUSB(w http.ResponseWriter, r *http.Request) {
	// Validate video ID before binary check so unknown IDs get 404, not 503.
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		http.Error(w, "ffmpeg is not installed — export is unavailable", http.StatusServiceUnavailable)
		return
	}

	src := video.FilePath()
	dir := filepath.Dir(src)
	stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	dstName := freeOutputName(dir, stem, "_usb", ".mp4")
	dst := filepath.Join(dir, dstName)

	if err := transcode.ExportUSB(r.Context(), s.convertSem, src, dst); err != nil {
		http.Error(w, "export failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if d, err := s.store.GetDirectory(r.Context(), video.DirectoryID); err == nil {
		s.startSyncDir(d)
	}
	fmt.Fprintf(w, `<span style="color:#4a9a4a;font-size:0.8rem">✓ Exported as %s</span>`, dstName)
}

// ── Trim ──────────────────────────────────────────────────────────────────────

func (s *server) handleTrim(w http.ResponseWriter, r *http.Request) {
	// Validate video ID before binary check so unknown IDs get 404, not 503.
	video, ok := s.videoOrError(w, r)
	if !ok {
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		http.Error(w, "ffmpeg is not installed — trimming is unavailable", http.StatusServiceUnavailable)
		return
	}

	start := strings.TrimSpace(r.FormValue("start"))
	end := strings.TrimSpace(r.FormValue("end"))
	if start == "" {
		start = "0"
	}

	src := video.FilePath()
	dir := filepath.Dir(src)
	ext := filepath.Ext(src)
	stem := strings.TrimSuffix(filepath.Base(src), ext)
	dstName := freeOutputName(dir, stem, "_trim", ext)
	dst := filepath.Join(dir, dstName)

	if err := transcode.Trim(r.Context(), s.convertSem, src, dst, start, end); err != nil {
		http.Error(w, "trim failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Upsert the new file synchronously so the client can open it immediately.
	if newVid, err := s.store.UpsertVideo(r.Context(), video.DirectoryID, dir, dstName); err == nil {
		ctx := r.Context()

		// Copy structured fields from the source video.
		fields := store.VideoFields{
			Genre:         video.Genre,
			SeasonNumber:  video.SeasonNumber,
			EpisodeNumber: video.EpisodeNumber,
			EpisodeTitle:  video.EpisodeTitle,
			Actors:        video.Actors,
			Studio:        video.Studio,
			Channel:       video.Channel,
			AirDate:       video.AirDate,
		}
		_ = s.store.UpdateVideoFields(ctx, newVid.ID, fields)

		// Copy display name (append " (trim)" if name was set).
		if video.DisplayName != "" {
			_ = s.store.UpdateVideoName(ctx, newVid.ID, video.DisplayName+" (trim)")
		}

		// Copy show name and video type.
		if video.ShowName != "" {
			_ = s.store.UpdateVideoShowName(ctx, newVid.ID, video.ShowName)
		}
		if video.VideoType != "" {
			_ = s.store.UpdateVideoType(ctx, newVid.ID, video.VideoType)
		}

		// Copy user-applied tags (skip system tags managed by VideoFields).
		if tags, err := s.store.ListTagsByVideo(ctx, video.ID); err == nil {
			for _, t := range tags {
				// System tags (namespace:value) are set via UpdateVideoFields above.
				if !strings.Contains(t.Name, ":") {
					if tagRec, err := s.store.UpsertTag(ctx, t.Name); err == nil {
						_ = s.store.TagVideo(ctx, newVid.ID, tagRec.ID)
					}
				}
			}
		}

		titleJSON, _ := json.Marshal(newVid.DisplayName)
		if newVid.DisplayName == "" {
			titleJSON, _ = json.Marshal(dstName)
		}
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"trimComplete":{"videoId":%d,"title":%s}}`, newVid.ID, titleJSON))
	}

	if d, err := s.store.GetDirectory(r.Context(), video.DirectoryID); err == nil {
		s.startSyncDir(d)
	}
	s.serveVideoList(w, r)
}
