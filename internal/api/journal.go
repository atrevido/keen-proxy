package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) auditLogin(r *http.Request, result, details string) {
	if j := s.D.Journal(journal.Audit); j != nil {
		j.Add(journal.Entry{Actor: "user", Action: "Panel login", Target: "auth", Result: result, Details: details + ", " + clientIP(r)})
	}
}

func (s *Server) journalOf(w http.ResponseWriter, r *http.Request) (journal.Kind, *journal.Journal, bool) {
	k := journal.Kind(r.PathValue("kind"))
	if !slices.Contains(journal.Kinds, k) {
		writeError(w, http.StatusNotFound, "unknown journal")
		return "", nil, false
	}
	j := s.D.Journal(k)
	if j == nil {
		writeError(w, http.StatusServiceUnavailable, "journal unavailable")
		return "", nil, false
	}
	return k, j, true
}

// journalPage returns newest-first entries with q, before, limit, and
// field-name filters such as outbound or level.
func (s *Server) journalPage(w http.ResponseWriter, r *http.Request) {
	k, j, ok := s.journalOf(w, r)
	if !ok {
		return
	}
	v := r.URL.Query()
	q := journal.Query{Text: v.Get("q"), Filters: map[string]string{}}
	q.Before, _ = strconv.ParseUint(v.Get("before"), 10, 64)
	q.Limit, _ = strconv.Atoi(v.Get("limit"))
	for _, f := range journal.Fields[k] {
		if x := v.Get(f); x != "" {
			q.Filters[f] = x
		}
	}
	writeJSON(w, http.StatusOK, j.Find(q, journal.Fields[k]))
}

// journalStream delivers new journal entries as server-sent events.
func (s *Server) journalStream(w http.ResponseWriter, r *http.Request) {
	_, j, ok := s.journalOf(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := j.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": ok\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			raw, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			// Batch writes to avoid flushing once per entry under load.
			for n := len(ch); n > 0; n-- {
				raw, _ = json.Marshal(<-ch)
				fmt.Fprintf(w, "data: %s\n\n", raw)
			}
			flusher.Flush()
		}
	}
}

// journalExport writes a redacted text export for since (default: 24h),
// excluding configured secrets and the panel token.
func (s *Server) journalExport(w http.ResponseWriter, r *http.Request) {
	k, j, ok := s.journalOf(w, r)
	if !ok {
		return
	}
	since := 24 * time.Hour
	if d, err := time.ParseDuration(r.URL.Query().Get("since")); err == nil && d > 0 {
		since = d
	}
	mask := s.D.Mask()
	token := s.Token
	full := func(t string) string {
		if token != "" {
			t = strings.ReplaceAll(t, token, "•••")
		}
		return mask(t)
	}
	name := fmt.Sprintf("keenproxy-%s-%s.log", k, time.Now().Format("20060102-1504"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if err := j.Export(w, k, time.Now().Add(-since), full); err != nil {
		s.Log.Warn("journal export", "err", err)
	}
}
