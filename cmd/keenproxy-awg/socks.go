package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/sagernet/sing/common/buf"
	sbufio "github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/protocol/socks/socks5"
)

type dialFunc func(context.Context, string, string) (net.Conn, error)

func serveSOCKS(ctx context.Context, listener net.Listener, allowed netip.Addr, password string, dial dialFunc) error {
	ctx, cancel := context.WithCancel(ctx)
	var active sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer func() {
		cancel()
		stop()
		active.Wait()
	}()
	slots := make(chan struct{}, 256)
	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("could not accept a SOCKS5 connection")
		}
		if !sourceAllowed(client, allowed) {
			_ = client.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
			active.Go(func() {
				defer func() { <-slots }()
				handleSOCKS(ctx, client, password, dial)
			})
		default:
			_ = client.Close()
		}
	}
}

func sourceAllowed(client net.Conn, allowed netip.Addr) bool {
	if !allowed.IsValid() {
		return true
	}
	remote, ok := client.RemoteAddr().(*net.TCPAddr)
	return ok && remote.AddrPort().Addr().Unmap() == allowed
}

func handleSOCKS(ctx context.Context, client net.Conn, password string, dial dialFunc) {
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	_ = client.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(client)
	if err := authenticate(client, reader, password); err != nil {
		return
	}
	request, err := readConnect(client, reader)
	if err != nil {
		return
	}
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	upstream, err := dial(timeout, "tcp", request.Destination.String())
	cancel()
	if err != nil {
		_ = socks5.WriteResponse(client, socks5.Response{ReplyCode: socks5.ReplyCodeForError(err)})
		return
	}
	defer upstream.Close()
	if err := socks5.WriteResponse(client, socks5.Response{ReplyCode: socks5.ReplyCodeSuccess}); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})
	relay := client
	if data, _ := reader.Peek(reader.Buffered()); len(data) > 0 {
		relay = sbufio.NewCachedConn(client, buf.As(data))
	}
	_ = sbufio.CopyConn(ctx, relay, upstream)
}

const socksUsername = "keenproxy"

func authenticate(client net.Conn, reader *bufio.Reader, password string) error {
	auth, err := socks5.ReadAuthRequest(reader)
	if err != nil {
		return err
	}
	method := socks5.AuthTypeNotRequired
	if password != "" {
		method = socks5.AuthTypeUsernamePassword
	}
	if !slices.Contains(auth.Methods, method) {
		_ = socks5.WriteAuthResponse(client, socks5.AuthResponse{Method: socks5.AuthTypeNoAcceptedMethods})
		return errors.New("SOCKS5 authentication method unavailable")
	}
	if err := socks5.WriteAuthResponse(client, socks5.AuthResponse{Method: method}); err != nil {
		return err
	}
	if method == socks5.AuthTypeNotRequired {
		return nil
	}
	return verifyCredentials(client, reader, password)
}

func verifyCredentials(client net.Conn, reader *bufio.Reader, password string) error {
	request, err := socks5.ReadUsernamePasswordAuthRequest(reader)
	if err != nil {
		return err
	}
	match := request.Username == socksUsername && subtle.ConstantTimeCompare([]byte(request.Password), []byte(password)) == 1
	status := socks5.UsernamePasswordStatusFailure
	if match {
		status = socks5.UsernamePasswordStatusSuccess
	}
	if err := socks5.WriteUsernamePasswordAuthResponse(client, socks5.UsernamePasswordAuthResponse{Status: status}); err != nil {
		return err
	}
	if !match {
		return errors.New("SOCKS5 authentication failed")
	}
	return nil
}

func readConnect(client net.Conn, reader *bufio.Reader) (socks5.Request, error) {
	request, err := socks5.ReadRequest(reader)
	if err != nil {
		return socks5.Request{}, err
	}
	if request.Command != socks5.CommandConnect {
		_ = socks5.WriteResponse(client, socks5.Response{ReplyCode: socks5.ReplyCodeUnsupported})
		return socks5.Request{}, errors.New("only SOCKS5 TCP CONNECT is supported")
	}
	return request, nil
}
