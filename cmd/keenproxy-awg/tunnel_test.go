package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
	"golang.org/x/net/proxy"
)

func TestTunnelCarriesTCPThroughSOCKS(t *testing.T) {
	requireIntegration(t)
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := startAWGPeer(t, clientKey)
	profile, err := amneziawg.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- withTunnel(ctx, profile, func(dial dialFunc) error {
			return serveSOCKS(ctx, listener, netip.MustParseAddr("127.0.0.1"), "fixture-password", dial)
		})
	}()
	dialer, err := proxy.SOCKS5("tcp", listener.Addr().String(), &proxy.Auth{User: "keenproxy", Password: "fixture-password"}, &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://198.18.0.2:8080/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(payload) != "AWG fixture payload" {
		t.Fatalf("unexpected origin response: %v, %v", response.StatusCode, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled SOCKS listener did not stop")
	}
}

func startAWGPeer(t *testing.T, client *ecdh.PrivateKey) string {
	t.Helper()
	server, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	protection := bytes.Repeat([]byte{0xab}, 32)
	interfaceTun, stack, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("198.18.0.2")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	peer := device.NewDevice(interfaceTun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	t.Cleanup(peer.Close)
	if err := peer.IpcSet(fmt.Sprintf("private_key=%x\ns1=16\ns2=16\ns3=16\ns4=16\nheader_protection_key=%x\npublic_key=%x\nallowed_ip=198.18.0.3/32\n", server.Bytes(), protection, client.PublicKey().Bytes())); err != nil {
		t.Fatal(err)
	}
	if err := peer.Up(); err != nil {
		t.Fatal(err)
	}
	settings, err := peer.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	var port string
	for line := range strings.SplitSeq(settings, "\n") {
		if value, ok := strings.CutPrefix(line, "listen_port="); ok {
			port = value
		}
	}
	listener, err := stack.ListenTCPAddrPort(netip.MustParseAddrPort("198.18.0.2:8080"))
	if err != nil {
		t.Fatal(err)
	}
	origin := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "AWG fixture payload")
	})}
	t.Cleanup(func() { _ = origin.Close() })
	go func() { _ = origin.Serve(listener) }()
	return fmt.Sprintf("[Interface]\nPrivateKey=%s\nAddress=198.18.0.3/32\nS1=16\nS2=16\nS3=16\nS4=16\nHeaderProtectionKey=%s\n[Peer]\nPublicKey=%s\nAllowedIPs=198.18.0.2/32\nEndpoint=127.0.0.1:%s\n",
		base64.StdEncoding.EncodeToString(client.Bytes()), base64.StdEncoding.EncodeToString(protection), base64.StdEncoding.EncodeToString(server.PublicKey().Bytes()), port)
}

func TestConfigCheckDoesNotDialOrListen(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "synthetic.conf")
	raw := `[Interface]
PrivateKey=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
Address=198.18.0.3/32
[Peer]
PublicKey=AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=
AllowedIPs=198.18.0.2/32
Endpoint=unresolvable.example.test:51820
`
	if err := os.WriteFile(profilePath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), options{config: profilePath, check: true}); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), options{config: profilePath, listen: "0.0.0.0:1080"}); err == nil || !strings.Contains(err.Error(), "-allow-from") {
		t.Fatal("non-loopback listener without an allow-list IP was not rejected")
	}
}

func TestSOCKSListenerDeniesConnectionsOutsideAllowedSource(t *testing.T) {
	requireIntegration(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := make(chan error, 1)
	go func() {
		stopped <- serveSOCKS(ctx, listener, netip.MustParseAddr("127.0.0.2"), "", nil)
	}()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	var reply [1]byte
	if count, err := client.Read(reply[:]); err == nil || count != 0 {
		t.Fatal("client outside the allow list reached SOCKS negotiation")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("client outside the allow list was not closed")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("SOCKS server did not stop")
	}
}

func TestListenerPolicyAllowsExplicitRouterAddress(t *testing.T) {
	bind, source, err := listenerPolicy("172.30.20.10:1080", "172.30.20.1")
	if err != nil || bind.String() != "172.30.20.10:1080" || source.String() != "172.30.20.1" {
		t.Fatalf("valid RouterOS listener policy was rejected: %v", err)
	}
}

func TestResolvePeerUsesIPv4ForDualStackLocalhost(t *testing.T) {
	addr, err := resolvePeer(t.Context(), "localhost:51820")
	if err != nil || addr.String() != "127.0.0.1:51820" {
		t.Fatalf("unexpected localhost endpoint: %v, %v", addr, err)
	}
}

func TestProbeRejectsInvalidTLSCertificate(t *testing.T) {
	requireIntegration(t)
	received := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	err := probe(t.Context(), func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	if err == nil {
		t.Fatal("probe accepted invalid TLS certificate")
	}
	select {
	case <-received:
		t.Fatal("probe sent HTTP on a connection with invalid TLS")
	default:
	}
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KEENPROXY_AWG_INTEGRATION") != "1" {
		t.Skip("set KEENPROXY_AWG_INTEGRATION=1 to allow loopback UDP and TCP listeners")
	}
}
