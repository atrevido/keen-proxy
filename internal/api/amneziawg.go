package api

import (
	"context"
	"net/http"

	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

// editableConfig includes a persisted draft when AmneziaWG activation is staged.
func (s *Server) editableConfig() *model.Config {
	if d, ok := s.D.(interface{ EditableConfig() *model.Config }); ok {
		return d.EditableConfig()
	}
	return s.D.Config()
}

// previewAmneziaWG validates the profile and optionally tests HTTPS through it.
func (s *Server) previewAmneziaWG(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Profile string `json:"profile"`
		Probe   bool   `json:"probe"`
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := decodeFeatureRequest(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid AmneziaWG preview request")
		return
	}
	profile, err := amneziawg.Parse(req.Profile)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Probe {
		d, ok := s.D.(interface {
			ProbeAmneziaWG(context.Context, string) error
		})
		if !ok {
			writeError(w, http.StatusNotImplemented, "AmneziaWG HTTPS probe is unavailable")
			return
		}
		if err := d.ProbeAmneziaWG(r.Context(), profile.Config); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"profile": profile.Config, "endpoint": profile.Endpoint})
}
