package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/atrevido/keenetic-proxy/internal/daemon"
	"io"
	"net/http"
	"time"
)

// diagnosticDaemon adds optional connectivity probes without changing failover health.
type diagnosticDaemon interface {
	DiagnosticChecks(context.Context, string) (daemon.DiagnosticReport, error)
}

// diagnosticCheck accepts one bounded request for an applied connection and hides raw errors.
func (s *Server) diagnosticCheck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Connection string `json:"connection"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil || dec.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid diagnostic request")
		return
	}
	found := false
	for _, c := range s.D.Config().Connections {
		if c.ID == input.Connection {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusBadRequest, "connection is not applied")
		return
	}
	d, ok := s.D.(diagnosticDaemon)
	if !ok {
		writeError(w, http.StatusNotImplemented, "diagnostic checks unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	report, err := d.DiagnosticChecks(ctx, input.Connection)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, daemon.ErrDiagnosticConnection) {
			status = http.StatusBadRequest
		}
		writeError(w, status, "connection check unavailable")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// DiagnosticChecks returns labeled synthetic results without making network requests.
func (d *Demo) DiagnosticChecks(_ context.Context, id string) (daemon.DiagnosticReport, error) {
	return daemon.DiagnosticReport{Connection: id, Demo: true, Results: []daemon.DiagnosticResult{
		{Kind: "http", OK: true, Checked: time.Now(), Latency: 42},
		{Kind: "https", OK: true, Checked: time.Now(), Latency: 58},
		{Kind: "transfer", OK: true, Checked: time.Now(), Latency: 144, Bytes: 65536},
	}}, nil
}
