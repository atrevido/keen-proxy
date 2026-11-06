package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/model"
	M "github.com/sagernet/sing/common/metadata"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDiagnosticBoundsAndFailures(t *testing.T) {
	client := &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme == "http" {
			return nil, errors.New("private-password must never reach UI")
		}
		size := 65536
		if r.URL.Host != "speed.cloudflare.com" {
			size = 0
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", size))), Header: make(http.Header)}, nil
	})}
	results := runDiagnosticChecks(context.Background(), client)
	if len(results) != 3 || results[0].OK || !results[1].OK || !results[2].OK || results[2].Bytes != 65536 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if strings.Contains(results[0].Error, "private-password") {
		t.Fatal("secret in diagnostic error")
	}
	if results[1].Error != "" || results[2].Error != "" {
		t.Fatal("successful diagnostic contains an error")
	}
}
func TestDiagnosticShortDownloadAndHTTPFailure(t *testing.T) {
	client := &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error")), Header: make(http.Header)}, nil
	})}
	for _, r := range runDiagnosticChecks(context.Background(), client) {
		if r.OK {
			t.Fatalf("HTTP503 accepted: %+v", r)
		}
	}
	client.Transport = diagnosticTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("short")), Header: make(http.Header)}, nil
	})
	if runDiagnosticChecks(context.Background(), client)[2].OK {
		t.Fatal("short download accepted")
	}
}
func TestDiagnosticCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
	for _, r := range runDiagnosticChecks(ctx, client) {
		if r.OK {
			t.Fatal("cancelled check passed")
		}
	}
}

func TestDiagnosticsRejectUnknownAndStoppedProfiles(t *testing.T) {
	d := &Daemon{cfg: &model.Config{Connections: []model.Connection{{ID: "one", Name: "One", Type: model.ConnLink}}}}
	for _, id := range []string{"missing", "one"} {
		if _, err := d.DiagnosticChecks(context.Background(), id); !errors.Is(err, ErrDiagnosticConnection) {
			t.Fatalf("%s: %v", id, err)
		}
	}
}
func TestDiagnosticProfileDialerAndCancellation(t *testing.T) {
	var profileContext context.Context
	client, peer := net.Pipe()
	defer peer.Close()
	dial := diagnosticDialer(context.Background(), func(ctx context.Context, network string, target M.Socksaddr) (net.Conn, error) {
		profileContext = ctx
		if network != "tcp" || target.Port != 443 || target.Addr.String() != "127.0.0.1" {
			t.Fatalf("wrong profile destination: %s %v", network, target)
		}
		return client, nil
	})
	conn, err := dial(context.Background(), "tcp", "127.0.0.1:443")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if profileContext.Err() != context.Canceled {
		t.Fatal("profile dial context leaked")
	}
	for _, address := range []string{"invalid", "127.0.0.1:bad"} {
		if _, err := dial(context.Background(), "tcp", address); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}
func TestDiagnosticSlotCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := acquireDiagnosticSlot(ctx); err != context.Canceled || release != nil {
		t.Fatal("cancelled check acquired a slot")
	}
}

func TestDiagnosticResponseBoundaries(t *testing.T) {
	for _, status := range []int{199, 200, 299, 300} {
		r := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("status response"))}
		count, _, err := diagnosticResponse(r, 0)
		if count != 0 || (err == nil) != (status >= 200 && status < 300) {
			t.Fatalf("status %d: %v", status, err)
		}
	}
	for _, size := range []int{0, 1, 65535, 65536, 65537, 70000} {
		r := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", size)))}
		count, _, err := diagnosticResponse(r, 65536)
		if (err == nil) != (size == 65536) || count > 65537 {
			t.Fatalf("size %d: count %d, %v", size, count, err)
		}
	}
}

func TestDiagnosticNetworkErrorsAreSanitized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if diagnosticNetworkError(ctx) != "check cancelled" {
		t.Fatal("cancellation lost")
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if diagnosticNetworkError(ctx) != "check timed out" {
		t.Fatal("deadline lost")
	}
	client := &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret") })}
	r := diagnosticRequest(context.Background(), client, diagnosticTest{kind: "https", url: "https://example.test"})
	if r.OK || r.Error != "network, DNS or TLS check failed" {
		t.Fatalf("unsafe result: %+v", r)
	}
	client.Transport = diagnosticTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	r = diagnosticRequest(context.Background(), client, diagnosticTest{kind: "https", url: "https://example.test"})
	if r.OK || r.Error != "HTTP 503" {
		t.Fatalf("HTTP status lost: %+v", r)
	}
}
