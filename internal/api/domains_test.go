package api

import (
	"context"
	"github.com/atrevido/keenetic-proxy/internal/daemon"
	"net/http"
	"testing"
)

func (f *fakeDaemon) Domains() daemon.DomainStatus { return daemon.DomainStatus{} }
func (f *fakeDaemon) RefreshDomains(ctx context.Context, names []string, preview bool) (daemon.DomainStatus, error) {
	return daemon.DomainStatus{}, nil
}
func TestDomainEndpoints(t *testing.T) {
	s, _ := newServer()
	h := s.Handler()
	for _, p := range []string{"/api/domains", "/api/domains/resolve"} {
		method := "GET"
		if p != "/api/domains" {
			method = "POST"
		}
		if do(t, h, method, p, `{"names":["example.com"]}`, "", lan).Code != http.StatusUnauthorized {
			t.Fatal("no auth", p)
		}
	}
	if w := do(t, h, "GET", "/api/domains", "", "secret", lan); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{}`, `{"names":["203.0.113.1"]}`, `{"names":["a.test"],"unknown":true}`} {
		if w := do(t, h, "POST", "/api/domains/resolve", body, "secret", lan); w.Code != 400 {
			t.Fatal(w.Code, body)
		}
	}
	if w := do(t, h, "POST", "/api/domains/resolve", `{"names":["example.com"],"preview":true}`, "secret", lan); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
