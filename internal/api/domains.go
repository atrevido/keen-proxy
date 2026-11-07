package api

import (
	"context"
	"encoding/json"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"io"
	"net/http"
	"time"
)

func (s *Server) domainStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.D.Domains())
}

// resolveDomains validates at most 100 domain names before a bounded live or preview refresh.
func (s *Server) resolveDomains(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names   []string `json:"names"`
		Preview bool     `json:"preview"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid DNS request")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, 400, "unexpected trailing data")
		return
	}
	if len(req.Names) == 0 || len(req.Names) > 100 {
		writeError(w, 400, "between 1 and 100 domains required")
		return
	}
	for _, name := range req.Names {
		e, err := model.ParseEntry(name)
		if err != nil || e.Kind != model.EntryDomain {
			writeError(w, 400, "domain required, not an IP or subnet")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	status, err := s.D.RefreshDomains(ctx, req.Names, req.Preview)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, status)
}
