package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/protocol/socks/socks5"
)

func TestProxyRejectsUDPWithoutOpeningTunnelConnection(t *testing.T) {
	client, dialed := startTestProxy(t, "")
	defer client.Close()
	reply := sendSOCKSRequest(t, client, socks5.CommandUDPAssociate)
	if reply != socks5.ReplyCodeUnsupported {
		t.Fatalf("UDP reply code = %d", reply)
	}
	select {
	case <-dialed:
		t.Fatal("UDP command opened a tunnel connection")
	default:
	}
}

func TestProxyFailsClosedWhenTunnelDialFails(t *testing.T) {
	client, dialed := startTestProxy(t, "")
	defer client.Close()
	reply := sendSOCKSRequest(t, client, socks5.CommandConnect)
	if reply == socks5.ReplyCodeSuccess {
		t.Fatal("proxy reported success without an AWG connection")
	}
	select {
	case destination := <-dialed:
		if destination != "198.18.0.2:8443" {
			t.Fatalf("unexpected tunnel destination %s", destination)
		}
	default:
		t.Fatal("TCP request did not attempt the AWG path")
	}
}

func startTestProxy(t *testing.T, password string) (net.Conn, <-chan string) {
	t.Helper()
	client, server := net.Pipe()
	deadline := time.Now().Add(3 * time.Second)
	_ = client.SetDeadline(deadline)
	dialed := make(chan string, 1)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = server.Close() })
	go handleSOCKS(ctx, server, password, func(_ context.Context, _, address string) (net.Conn, error) {
		dialed <- address
		return nil, errors.New("tunnel unavailable")
	})
	return client, dialed
}

func sendSOCKSRequest(t *testing.T, client net.Conn, command byte) byte {
	t.Helper()
	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var auth [2]byte
	if _, err := io.ReadFull(client, auth[:]); err != nil || auth != [2]byte{5, 0} {
		t.Fatalf("SOCKS negotiation failed: %v, %v", auth, err)
	}
	request := socks5.Request{Command: command, Destination: M.ParseSocksaddr("198.18.0.2:8443")}
	if err := socks5.WriteRequest(client, request); err != nil {
		t.Fatal(err)
	}
	reply, err := socks5.ReadResponse(bufio.NewReader(client))
	if err != nil {
		t.Fatal(err)
	}
	return reply.ReplyCode
}

func TestProxyDeniesAnonymousOrWrongCredentials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  byte
		request *socks5.UsernamePasswordAuthRequest
	}{
		{"anonymous", socks5.AuthTypeNotRequired, nil},
		{"wrong password", socks5.AuthTypeUsernamePassword, &socks5.UsernamePasswordAuthRequest{Username: "keenproxy", Password: "wrong"}},
		{"wrong user", socks5.AuthTypeUsernamePassword, &socks5.UsernamePasswordAuthRequest{Username: "wrong", Password: "secret"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, dialed := startTestProxy(t, "secret")
			defer client.Close()
			reader := bufio.NewReader(client)
			if err := socks5.WriteAuthRequest(client, socks5.AuthRequest{Methods: []byte{tc.method}}); err != nil {
				t.Fatal(err)
			}
			auth, err := socks5.ReadAuthResponse(reader)
			if err != nil {
				t.Fatal(err)
			}
			if tc.request == nil {
				if auth.Method != socks5.AuthTypeNoAcceptedMethods {
					t.Fatal("anonymous method accepted")
				}
				return
			}
			if auth.Method != socks5.AuthTypeUsernamePassword {
				t.Fatalf("unexpected authentication method %d", auth.Method)
			}
			if err := socks5.WriteUsernamePasswordAuthRequest(client, *tc.request); err != nil {
				t.Fatal(err)
			}
			response, err := socks5.ReadUsernamePasswordAuthResponse(reader)
			if err != nil || response.Status != socks5.UsernamePasswordStatusFailure {
				t.Fatalf("invalid credentials accepted: %v, %v", response, err)
			}
			connect := socks5.Request{Command: socks5.CommandConnect, Destination: M.ParseSocksaddr("198.18.0.2:8443")}
			if err := socks5.WriteRequest(client, connect); err == nil {
				t.Fatal("server continued reading requests after authentication failed")
			}
			select {
			case <-dialed:
				t.Fatal("invalid credentials reached the AWG tunnel")
			default:
			}
		})
	}
}
