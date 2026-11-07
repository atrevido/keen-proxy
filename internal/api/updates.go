package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/ordo-systems/keen-proxy/internal/update"
)

var updateOrigin = http.NewCrossOriginProtection()

// updateRoutes separates release discovery from operations that can replace the binary.
func (s *Server) updateRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/updates", s.auth(s.updateStatus))
	mux.Handle("POST /api/updates/check", s.auth(s.checkUpdates))
	mux.Handle("PUT /api/updates/preferences", s.updateAuthorization(s.updatePreferences))
	mux.Handle("POST /api/updates/install", s.updateAuthorization(s.installUpdate))
}

func (s *Server) updateAuthorization(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenOK(r) {
			writeError(w, http.StatusForbidden, "panel token required to manage updates")
			return
		}
		if err := updateOrigin.Check(r); err != nil {
			writeError(w, http.StatusForbidden, "cross-origin update request denied")
			return
		}
		h(w, r)
	}
}

func (s *Server) updateStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.Updates.Status())
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	s.Updates.Check(r.Context(), true)
	s.updateStatus(w, r)
}

func (s *Server) updatePreferences(w http.ResponseWriter, r *http.Request) {
	var p update.Preferences
	if !readUpdateJSON(w, r, &p) {
		return
	}
	if err := s.Updates.SetPreferences(p); err != nil {
		s.updateError(w, err)
		return
	}
	s.updateStatus(w, r)
}

func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target   string `json:"target"`
		Rollback bool   `json:"rollback"`
	}
	if !readUpdateJSON(w, r, &req) {
		return
	}
	if err := s.Updates.Start(req.Target, req.Rollback); err != nil {
		s.updateError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, s.Updates.Status())
}

func readUpdateJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "expected a JSON update request")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid update request")
		return false
	}
	return true
}

func (s *Server) updateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, update.ErrPreferences):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, update.ErrUnavailable), errors.Is(err, update.ErrTarget), errors.Is(err, update.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	default:
		if s.Log != nil {
			s.Log.Warn("could not manage KeenProxy update", "err", err)
		}
		writeError(w, http.StatusInternalServerError, "could not manage the update")
	}
}
