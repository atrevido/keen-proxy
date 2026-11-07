package api

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/catalog"
	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/filterlist"
	"github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"presets": catalog.Presets, "dns": catalog.DNS, "filters": catalog.Filters, "geoip": geoip.Builtins})
}

// presetPreview updates a copied draft and returns its entry diff without applying it.
func (s *Server) presetPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Config          *model.Config `json:"config"`
		ID              string        `json:"id"`
		Target          string        `json:"target"`
		Index           *int          `json:"index"`
		IncludePrefixes bool          `json:"include_prefixes"`
	}
	if err := decodeFeatureRequest(w, r, &req); err != nil || req.Config == nil {
		writeError(w, 400, "config and id required")
		return
	}
	p, ok := catalog.Find(req.ID)
	if !ok {
		writeError(w, 400, "unknown preset")
		return
	}
	if req.IncludePrefixes {
		p.Domains = append(append([]string{}, p.Domains...), p.Prefixes...)
	}
	c := req.Config.Clone()
	g := model.Group{Name: p.Name, Target: req.Target}
	index := len(c.Groups)
	if req.Index != nil {
		index = *req.Index
		if index < 0 || index >= len(c.Groups) {
			writeError(w, 400, "group index out of range")
			return
		}
		g = c.Groups[index]
		if g.Preset == nil || g.Preset.ID != req.ID {
			writeError(w, 400, "different group preset")
			return
		}
	}
	g, diff := catalog.Update(g, p)
	if req.Index == nil {
		c.Groups = append(c.Groups, g)
	} else {
		c.Groups[index] = g
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"config": c, "diff": diff})
}

// featureDaemon exposes optional DNS probes and runtime blocklist management.
type featureDaemon interface {
	DNSInfo() daemon.DNSInfo
	FilterStatus() []filterlist.State
	RefreshFilters(context.Context) error
	CheckDNS(context.Context, *model.Config) (daemon.DNSCheck, error)
}

func (s *Server) features(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.D.(featureDaemon); ok {
		writeJSON(w, 200, map[string]any{"dns": d.DNSInfo(), "filters": d.FilterStatus()})
		return
	}
	writeJSON(w, 200, map[string]any{"demo": true, "dns": daemon.DNSInfo{}, "filters": []filterlist.State{}})
}

// refreshFilters bounds applied-source downloads to two minutes and returns updated states.
// Demo implementations return a labeled response without network activity.
func (s *Server) refreshFilters(w http.ResponseWriter, r *http.Request) {
	d, ok := s.D.(featureDaemon)
	if !ok {
		writeJSON(w, 200, map[string]any{"demo": true, "message": "Demo: list downloads are disabled"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := d.RefreshFilters(ctx); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, d.FilterStatus())
}

// checkDNS validates draft DNS settings but requires referenced connection profiles to be applied.
func (s *Server) checkDNS(w http.ResponseWriter, r *http.Request) {
	var c model.Config
	if err := decodeFeatureRequest(w, r, &c); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	d, ok := s.D.(featureDaemon)
	if !ok {
		writeJSON(w, 200, daemon.DNSCheck{Demo: true, Route: "demo", Error: "Demo: network requests are disabled"})
		return
	}
	// Connection profiles in a draft are not running yet. Avoid testing another
	// live profile with the same id while presenting the draft as verified.
	live := s.D.Config()
	if c.DNS.Route == "connection" {
		if !reflect.DeepEqual(c.Connections, live.Connections) {
			writeError(w, 400, "Apply connection changes first")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := d.CheckDNS(ctx, &c)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
