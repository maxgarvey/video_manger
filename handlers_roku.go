// handlers_roku.go – cast-to-Roku endpoints.
//
// POST /roku/cast/{id}  – web UI queues a video to be played on the Roku.
// GET  /roku/poll       – Roku calls this; receives the pending video (once)
//
//	and returns 204 when the queue is empty.
//
// GET  /roku/connected  – returns 200 if the Roku polled recently, 204 if not.
package main

import (
	"net/http"
	"time"
)

const rokuLivePeriod = 6 * time.Second // Roku polls every 2 s; 6 s = 3 missed polls

// rokuDisabled checks the roku_enabled setting; returns true (and writes 404)
// when Roku connectivity is turned off.
func (s *server) rokuDisabled(w http.ResponseWriter, r *http.Request) bool {
	v, _ := s.store.GetSetting(r.Context(), "roku_enabled")
	if v != "true" {
		http.NotFound(w, r)
		return true
	}
	return false
}

// handleRokuCast queues a video for the Roku to pick up.
// The Roku polls /roku/poll every few seconds; the command expires after 30 s
// if the Roku never fetches it.
func (s *server) handleRokuCast(w http.ResponseWriter, r *http.Request) {
	if s.rokuDisabled(w, r) {
		return
	}
	id, ok := parseIDParam(w, r)
	if !ok {
		return
	}
	s.castMu.Lock()
	s.castVideoID = id
	s.castPostedAt = time.Now()
	s.castMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleRokuConnected returns 200 {"connected":true} if the Roku has polled
// recently, or 204 if not.  The web UI polls this to show/hide the Cast button.
func (s *server) handleRokuConnected(w http.ResponseWriter, r *http.Request) {
	if s.rokuDisabled(w, r) {
		return
	}
	s.castMu.Lock()
	last := s.castLastPoll
	s.castMu.Unlock()
	if time.Since(last) > rokuLivePeriod {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, map[string]bool{"connected": true})
}

// handleRokuPoll is called by the Roku app on a short interval.
// Returns the pending video as JSON and clears the queue, or 204 if empty.
func (s *server) handleRokuPoll(w http.ResponseWriter, r *http.Request) {
	if s.rokuDisabled(w, r) {
		return
	}
	s.castMu.Lock()
	s.castLastPoll = time.Now()
	id := s.castVideoID
	posted := s.castPostedAt
	if id != 0 {
		s.castVideoID = 0
		s.castPostedAt = time.Time{}
	}
	s.castMu.Unlock()

	if id == 0 || time.Since(posted) > 30*time.Second {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	v, err := s.store.GetVideo(r.Context(), id)
	if err != nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}
	writeJSON(w, videoToAPI(v))
}
