package daemon

import (
	"context"
	"errors"
	"fmt"
	"github.com/ordo-systems/keen-proxy/internal/model"
	M "github.com/sagernet/sing/common/metadata"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// DiagnosticResult reports one connectivity or transfer check with elapsed milliseconds.
type DiagnosticResult struct {
	Kind    string    `json:"kind"`
	OK      bool      `json:"ok"`
	Checked time.Time `json:"checked"`
	Latency int64     `json:"latency_ms"`
	Bytes   int64     `json:"bytes,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// DiagnosticReport groups checks performed through one applied connection profile.
type DiagnosticReport struct {
	Connection string             `json:"connection"`
	Results    []DiagnosticResult `json:"results"`
	Demo       bool               `json:"demo,omitempty"`
}

// ErrDiagnosticConnection means the requested profile is absent from applied runtime state.
var ErrDiagnosticConnection = errors.New("connection is not applied")
var diagnosticSlots = make(chan struct{}, 2)

const diagnosticTimeout = 8 * time.Second

// acquireDiagnosticSlot bounds concurrent reports and returns a release function.
func acquireDiagnosticSlot(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case diagnosticSlots <- struct{}{}:
		return func() { <-diagnosticSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DiagnosticChecks dials the actual profile, never its failover selector, and
// does not update periodic health, configuration or route selection.
func (d *Daemon) DiagnosticChecks(ctx context.Context, id string) (DiagnosticReport, error) {
	report := DiagnosticReport{Connection: id}
	cfg := d.Config()
	if cfg == nil || !slices.ContainsFunc(cfg.Connections, func(c model.Connection) bool { return c.ID == id }) {
		return report, ErrDiagnosticConnection
	}
	release, err := acquireDiagnosticSlot(ctx)
	if err != nil {
		return report, err
	}
	defer release()
	out, engineContext, ok := d.eng.Outbound(id)
	if !ok {
		return report, ErrDiagnosticConnection
	}
	transport := &http.Transport{DialContext: diagnosticDialer(engineContext, out.DialContext), DisableKeepAlives: true, ResponseHeaderTimeout: diagnosticTimeout, TLSHandshakeTimeout: diagnosticTimeout, DisableCompression: true}
	defer transport.CloseIdleConnections()
	report.Results = runDiagnosticChecks(ctx, &http.Client{Transport: transport, Timeout: diagnosticTimeout})
	return report, nil
}

// profileDial opens a connection through one specific applied engine outbound.
type profileDial func(context.Context, string, M.Socksaddr) (net.Conn, error)

// diagnosticTarget resolves an HTTP destination through the local IPv4 resolver.
func diagnosticTarget(ctx context.Context, address string) (M.Socksaddr, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return M.Socksaddr{}, err
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return M.Socksaddr{}, err
	}
	// Control-plane DNS is explicit; these checks do not certify DNS leakage.
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return M.Socksaddr{}, errors.New("diagnostic DNS lookup failed")
	}
	return M.SocksaddrFrom(ips[0].Unmap(), uint16(port)), nil
}

// diagnosticDialer combines engine services with request cancellation and releases
// context resources when the connection closes.
func diagnosticDialer(engineContext context.Context, dial profileDial) func(context.Context, string, string) (net.Conn, error) {
	return func(requestContext context.Context, network, address string) (net.Conn, error) {
		target, err := diagnosticTarget(requestContext, address)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(engineContext)
		stop := context.AfterFunc(requestContext, cancel)
		cleanup := func() { stop(); cancel() }
		conn, err := dial(ctx, network, target)
		if err != nil {
			cleanup()
			return nil, err
		}
		return &diagnosticConn{Conn: conn, cleanup: cleanup}, nil
	}
}

// diagnosticTest specifies a probe URL and optional exact transfer length.
type diagnosticTest struct {
	kind, url string
	bytes     int64
}

var diagnosticTests = []diagnosticTest{
	{"http", "http://connectivitycheck.gstatic.com/generate_204", 0},
	{"https", "https://www.gstatic.com/generate_204", 0},
	{"transfer", "https://speed.cloudflare.com/__down?bytes=65536", 65536},
}

// runDiagnosticChecks runs probes sequentially with redirects disabled.
func runDiagnosticChecks(ctx context.Context, original *http.Client) []DiagnosticResult {
	client := *original
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	results := make([]DiagnosticResult, 0, len(diagnosticTests))
	for _, test := range diagnosticTests {
		results = append(results, diagnosticRequest(ctx, &client, test))
	}
	return results
}
func requestDiagnostic(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

// diagnosticResponse requires a successful HTTP status and, for transfers, an
// exact bounded byte count. It always closes the response body.
func diagnosticResponse(response *http.Response, want int64) (int64, string, error) {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		reason := fmt.Sprintf("HTTP %d", response.StatusCode)
		return 0, reason, errors.New(reason)
	}
	if want == 0 {
		return 0, "", nil
	}
	count, err := io.Copy(io.Discard, io.LimitReader(response.Body, want+1))
	if err == nil && count != want {
		return count, "unexpected response length", errors.New("unexpected response length")
	}
	return count, "", err
}
func diagnosticNetworkError(ctx context.Context) string {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return "check timed out"
	case context.Canceled:
		return "check cancelled"
	default:
		return "network, DNS or TLS check failed"
	}
}

// diagnosticRequest enforces one probe deadline and returns sanitized failure details.
func diagnosticRequest(ctx context.Context, client *http.Client, test diagnosticTest) DiagnosticResult {
	result := DiagnosticResult{Kind: test.kind, Checked: time.Now()}
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, diagnosticTimeout)
	defer cancel()
	response, err := requestDiagnostic(ctx, client, test.url)
	if err == nil {
		result.Bytes, result.Error, err = diagnosticResponse(response, test.bytes)
	}
	result.Latency = time.Since(start).Milliseconds()
	result.OK = err == nil
	// Never expose raw engine errors: they may contain profile credentials.
	if err != nil && result.Error == "" {
		result.Error = diagnosticNetworkError(ctx)
	}
	return result
}

// diagnosticConn couples a transport connection to its request-context cleanup.
type diagnosticConn struct {
	net.Conn
	cleanup func()
}

// Close releases the socket and detaches the request cancellation callback.
func (c *diagnosticConn) Close() error { defer c.cleanup(); return c.Conn.Close() }
