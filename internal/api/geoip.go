package api

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/daemon"
)

// geoDaemon manages country databases of the applied configuration.
type geoDaemon interface {
	GeoDatabases() []daemon.GeoDatabaseInfo
	UpdateGeo(ctx context.Context, id string) error
	LookupGeo(ip netip.Addr, id string) (daemon.GeoLookup, error)
}

// geoResponse lists database states; Demo marks synthetic data without downloads.
type geoResponse struct {
	Demo      bool                     `json:"demo,omitempty"`
	Databases []daemon.GeoDatabaseInfo `json:"databases"`
	Error     string                   `json:"error,omitempty"`
}

func (s *Server) geoDaemon(w http.ResponseWriter) (geoDaemon, bool) {
	d, ok := s.D.(geoDaemon)
	if !ok {
		writeError(w, 501, "IP databases are unavailable")
	}
	return d, ok
}

func (s *Server) geoDatabases(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.geoDaemon(w); ok {
		_, demo := s.D.(*Demo)
		writeJSON(w, 200, geoResponse{Demo: demo, Databases: d.GeoDatabases()})
	}
}

// updateGeo downloads one applied database (even a disabled one) or all
// enabled databases. Download errors are returned with the updated states.
func (s *Server) updateGeo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeFeatureRequest(w, r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	d, ok := s.geoDaemon(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	_, demo := s.D.(*Demo)
	res := geoResponse{Demo: demo}
	if err := d.UpdateGeo(ctx, req.ID); err != nil {
		res.Error = err.Error()
	}
	res.Databases = d.GeoDatabases()
	writeJSON(w, 200, res)
}

// lookupGeo tests an address against the enabled databases, or one database by ID.
func (s *Server) lookupGeo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
		ID string `json:"id"`
	}
	if err := decodeFeatureRequest(w, r, &req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(req.IP))
	if err != nil || ip.Zone() != "" {
		writeError(w, 400, "an IPv4 or IPv6 address is required")
		return
	}
	d, ok := s.geoDaemon(w)
	if !ok {
		return
	}
	res, err := d.LookupGeo(ip, req.ID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, res)
}
