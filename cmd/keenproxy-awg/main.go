// Command keenproxy-awg runs one native AmneziaWG profile as a TCP SOCKS5 proxy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
	_ "golang.org/x/crypto/x509roots/fallback"
)

type options struct {
	config    string
	listen    string
	allowFrom string
	password  string
	check     bool
	probe     bool
}

func main() {
	var opts options
	flag.StringVar(&opts.config, "config", "", "path to the native AmneziaWG profile")
	flag.StringVar(&opts.listen, "socks-listen", "127.0.0.1:1080", "SOCKS5 TCP listen address")
	flag.StringVar(&opts.allowFrom, "allow-from", "", "only accept SOCKS5 connections from this IP (required on non-loopback)")
	flag.BoolVar(&opts.check, "check", false, "validate the profile and exit")
	flag.BoolVar(&opts.probe, "probe", false, "verify a fixed HTTPS endpoint through the tunnel and exit")
	flag.Parse()
	opts.password = os.Getenv("KEENPROXY_AWG_SOCKS_PASSWORD")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, opts)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	if opts.check && opts.probe {
		return errors.New("-check and -probe cannot be combined")
	}
	profile, err := loadProfile(opts.config)
	if err != nil || opts.check {
		return err
	}
	if opts.probe {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return withTunnel(ctx, profile, func(dial dialFunc) error { return probe(ctx, dial) })
	}
	bind, allowed, err := listenerPolicy(opts.listen, opts.allowFrom)
	if err != nil {
		return err
	}
	if len(opts.password) > 255 {
		return errors.New("SOCKS5 authentication password exceeds 255 bytes")
	}
	return withTunnel(ctx, profile, func(dial dialFunc) error {
		listener, err := net.Listen("tcp", bind.String())
		if err != nil {
			return errors.New("could not open the SOCKS5 listener")
		}
		defer listener.Close()
		return serveSOCKS(ctx, listener, allowed, opts.password, dial)
	})
}

func loadProfile(path string) (amneziawg.Profile, error) {
	file, err := os.Open(path)
	if err != nil {
		return amneziawg.Profile{}, errors.New("could not open the AmneziaWG profile")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, amneziawg.MaxProfileSize+1))
	if err != nil {
		return amneziawg.Profile{}, errors.New("could not read the AmneziaWG profile")
	}
	return amneziawg.Parse(string(raw))
}

func listenerPolicy(address, allowedFrom string) (netip.AddrPort, netip.Addr, error) {
	bind, err := netip.ParseAddrPort(address)
	if err != nil || bind.Port() == 0 || bind.Addr().Zone() != "" {
		return netip.AddrPort{}, netip.Addr{}, errors.New("invalid SOCKS5 listen IP or port")
	}
	if allowedFrom == "" && bind.Addr().IsLoopback() {
		return bind, netip.Addr{}, nil
	}
	allowed, err := allowSource(allowedFrom)
	if err != nil {
		return netip.AddrPort{}, netip.Addr{}, err
	}
	return bind, allowed, nil
}

func allowSource(value string) (netip.Addr, error) {
	allowed, err := netip.ParseAddr(value)
	if err != nil || allowed.Zone() != "" || !(allowed.IsGlobalUnicast() || allowed.IsLoopback()) {
		return netip.Addr{}, errors.New("SOCKS5 listener requires a valid -allow-from IP")
	}
	return allowed.Unmap(), nil
}

func withTunnel(ctx context.Context, profile amneziawg.Profile, use func(dialFunc) error) error {
	endpoint, err := resolvePeer(ctx, profile.Endpoint)
	if err != nil {
		return err
	}
	tun, stack, err := netstack.CreateNetTUN(profile.Addresses, tunnelDNS(profile), profile.MTU)
	if err != nil {
		return errors.New("could not initialize the AmneziaWG network stack")
	}
	client := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	defer client.Close()
	if err := client.IpcSet(profile.IPC(endpoint)); err != nil {
		return errors.New("AmneziaWG rejected the device configuration")
	}
	if err := client.Up(); err != nil {
		return errors.New("could not start the AmneziaWG device")
	}
	return use(stack.DialContext)
}

func resolvePeer(ctx context.Context, endpoint string) (netip.AddrPort, error) {
	if addr, err := netip.ParseAddrPort(endpoint); err == nil {
		return addr, nil
	}
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return netip.AddrPort{}, errors.New("invalid AmneziaWG endpoint")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return netip.AddrPort{}, errors.New("could not resolve the AmneziaWG endpoint")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return netip.AddrPort{}, errors.New("invalid AmneziaWG endpoint port")
	}
	for _, addr := range addrs {
		if addr.Unmap().Is4() {
			return netip.AddrPortFrom(addr.Unmap(), uint16(number)), nil
		}
	}
	return netip.AddrPortFrom(addrs[0], uint16(number)), nil
}

// tunnelDNS keeps proxy destination resolution inside the tunnel, even for a
// native profile that did not specify its own DNS addresses.
func tunnelDNS(profile amneziawg.Profile) []netip.Addr {
	if len(profile.DNS) > 0 {
		return profile.DNS
	}
	var dns []netip.Addr
	if slices.ContainsFunc(profile.Addresses, netip.Addr.Is4) {
		dns = append(dns, netip.MustParseAddr("1.1.1.1"))
	}
	if slices.ContainsFunc(profile.Addresses, netip.Addr.Is6) {
		dns = append(dns, netip.MustParseAddr("2606:4700:4700::1111"))
	}
	return dns
}

func probe(ctx context.Context, dial dialFunc) error {
	transport := &http.Transport{DialContext: dial, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cp.cloudflare.com/generate_204", nil)
	if err != nil {
		return errors.New("could not create the AmneziaWG HTTPS probe")
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("AmneziaWG HTTPS probe failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("AmneziaWG HTTPS probe returned an unexpected status")
	}
	return nil
}
