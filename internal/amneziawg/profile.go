// Package amneziawg imports single-peer native AmneziaWG client profiles.
package amneziawg

import (
	"bufio"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MaxProfileSize limits the amount of text accepted at import and startup.
const MaxProfileSize = 64 * 1024

// Profile contains validated configuration. Config includes tunnel keys but
// excludes comments and subscription credentials. ClientID is the public key.
type Profile struct {
	Config    string
	Endpoint  string
	ClientID  string
	Addresses []netip.Addr
	DNS       []netip.Addr
	MTU       int
	ipc       string
}

type profileSection struct {
	name   string
	fields []field
	values map[string]string
}

type field struct {
	name string
	ipc  string
}

var interfaceFields = []field{
	{"PrivateKey", ""}, {"Address", ""}, {"DNS", ""}, {"MTU", ""}, {"ListenPort", "listen_port"},
	{"Jc", "jc"}, {"Jmin", "jmin"}, {"Jmax", "jmax"},
	{"S1", "s1"}, {"S2", "s2"}, {"S3", "s3"}, {"S4", "s4"},
	{"H1", "h1"}, {"H2", "h2"}, {"H3", "h3"}, {"H4", "h4"},
	{"I1", "i1"}, {"I2", "i2"}, {"I3", "i3"}, {"I4", "i4"}, {"I5", "i5"},
	{"HeaderProtectionKey", ""}, {"ContentPaddingAddition", "content_padding_addition"},
	{"RekeyAfterTime", "rekey_after_time"}, {"RekeyTimeout", "rekey_timeout"}, {"RejectAfterTime", "reject_after_time"},
	{"KeepaliveTimeout", "keepalive_timeout"}, {"MaxHandshakeAttempts", "max_handshake_attempts"},
	{"RandomTrailers", "random_trailers"}, {"DisableCookies", "disable_cookies"},
}

var peerFields = []field{
	{"PublicKey", ""}, {"PresharedKey", ""}, {"AllowedIPs", ""}, {"Endpoint", ""},
	{"PersistentKeepalive", "persistent_keepalive_interval"},
}

func newSection(name string, fields []field) *profileSection {
	return &profileSection{name: name, fields: fields, values: make(map[string]string)}
}

func (s *profileSection) set(line string) bool {
	key, value, found := strings.Cut(line, "=")
	value = strings.TrimSpace(value)
	if !found || value == "" || strings.IndexFunc(value, func(r rune) bool { return r < 32 && r != '\t' || r == 127 }) >= 0 {
		return false
	}
	for _, f := range s.fields {
		if !strings.EqualFold(f.name, strings.TrimSpace(key)) {
			continue
		}
		if _, exists := s.values[f.name]; exists {
			return false
		}
		s.values[f.name] = value
		return true
	}
	return false
}

func (s *profileSection) appendINI(out *strings.Builder) {
	fmt.Fprintf(out, "[%s]\n", s.name)
	for _, f := range s.fields {
		if value, ok := s.values[f.name]; ok {
			fmt.Fprintf(out, "%s = %s\n", f.name, value)
		}
	}
}

func (s *profileSection) appendIPC(out *strings.Builder) {
	for _, f := range s.fields {
		if value, ok := s.values[f.name]; f.ipc != "" && ok {
			fmt.Fprintf(out, "%s=%s\n", f.ipc, value)
		}
	}
}

// Parse validates the supported native client profile and removes provider
// metadata before it can enter the persistent configuration.
func Parse(raw string) (Profile, error) {
	if len(raw) > MaxProfileSize {
		return Profile{}, errors.New("AmneziaWG profile is too large")
	}
	intf, peer, err := scanProfile(raw)
	if err != nil {
		return Profile{}, err
	}
	return compileProfile(intf, peer)
}

func scanProfile(raw string) (*profileSection, *profileSection, error) {
	intf := newSection("Interface", interfaceFields)
	peer := newSection("Peer", peerFields)
	var current *profileSection
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(raw, "\ufeff")))
	for lineNum := 1; scanner.Scan(); lineNum++ {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		line = strings.TrimSpace(line)
		if ignorable(line) {
			continue
		}
		next := readProfileLine(current, intf, peer, line)
		if next == nil {
			return nil, nil, fmt.Errorf("unsupported or duplicate AmneziaWG setting at line %d", lineNum)
		}
		current = next
	}
	if scanner.Err() != nil || current != peer {
		return nil, nil, errors.New("AmneziaWG profile requires one Interface and one Peer")
	}
	return intf, peer, nil
}

func ignorable(line string) bool {
	return line == "" || strings.HasPrefix(line, ";") || isSubscription(line)
}

func readProfileLine(current, intf, peer *profileSection, line string) *profileSection {
	switch {
	case current == nil && strings.EqualFold(line, "[Interface]"):
		return intf
	case current == intf && strings.EqualFold(line, "[Peer]"):
		return peer
	case current != nil && current.set(line):
		return current
	default:
		return nil
	}
}

func isSubscription(line string) bool {
	if strings.HasPrefix(line, "vpn://") {
		return true
	}
	key, value, found := strings.Cut(line, "=")
	if !found || !strings.HasPrefix(strings.TrimSpace(value), "vpn://") {
		return false
	}
	return slices.Contains([]string{"vpn", "subscriptionkey", "subscription_key"}, strings.ToLower(strings.TrimSpace(key)))
}

func compileProfile(intf, peer *profileSection) (Profile, error) {
	keys, err := readKeys(intf.values, peer.values)
	if err != nil {
		return Profile{}, err
	}
	profile, err := readNetwork(intf.values, peer.values)
	if err != nil {
		return Profile{}, err
	}
	if err := validateOptions(intf.values, peer.values); err != nil {
		return Profile{}, err
	}
	var ini, ipc strings.Builder
	intf.appendINI(&ini)
	ini.WriteByte('\n')
	peer.appendINI(&ini)
	profile.Config = ini.String()
	profile.ClientID = hex.EncodeToString(keys.client.PublicKey().Bytes())

	fmt.Fprintf(&ipc, "private_key=%x\n", keys.client.Bytes())
	if keys.header != nil {
		fmt.Fprintf(&ipc, "header_protection_key=%x\n", keys.header)
	}
	intf.appendIPC(&ipc)
	fmt.Fprintf(&ipc, "public_key=%x\n", keys.peer)
	if keys.psk != nil {
		fmt.Fprintf(&ipc, "preshared_key=%x\n", keys.psk)
	}
	for ip := range strings.SplitSeq(peer.values["AllowedIPs"], ",") {
		fmt.Fprintf(&ipc, "allowed_ip=%s\n", strings.TrimSpace(ip))
	}
	peer.appendIPC(&ipc)
	profile.ipc = ipc.String()
	return profile, nil
}

// IPC builds userspace device settings with the already-resolved peer address.
func (p Profile) IPC(peer netip.AddrPort) string {
	return p.ipc + "endpoint=" + peer.String() + "\n"
}

type profileKeys struct {
	client *ecdh.PrivateKey
	peer   []byte
	psk    []byte
	header []byte
}

func readKeys(intf, peer map[string]string) (profileKeys, error) {
	var result profileKeys
	private, err := readKey(intf["PrivateKey"], "PrivateKey", false)
	if err != nil {
		return result, err
	}
	result.client, err = ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return result, errors.New("invalid AmneziaWG PrivateKey")
	}
	for _, key := range []struct {
		value, name string
		target      *[]byte
		optional    bool
	}{
		{peer["PublicKey"], "PublicKey", &result.peer, false},
		{peer["PresharedKey"], "PresharedKey", &result.psk, true},
		{intf["HeaderProtectionKey"], "HeaderProtectionKey", &result.header, true},
	} {
		*key.target, err = readKey(key.value, key.name, key.optional)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func readKey(value, name string, optional bool) ([]byte, error) {
	if optional && value == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(key) != 32 || name != "PresharedKey" && !slices.ContainsFunc(key, func(b byte) bool { return b != 0 }) {
		return nil, fmt.Errorf("invalid AmneziaWG %s", name)
	}
	return key, nil
}

func readNetwork(intf, peer map[string]string) (Profile, error) {
	var profile Profile
	var err error
	profile.Addresses, err = addresses(intf["Address"], true)
	if err != nil {
		return Profile{}, fmt.Errorf("invalid AmneziaWG Address: %w", err)
	}
	if dns := intf["DNS"]; dns != "" {
		profile.DNS, err = addresses(dns, false)
		if err != nil {
			return Profile{}, fmt.Errorf("invalid AmneziaWG DNS: %w", err)
		}
	}
	profile.MTU, err = readMTU(intf["MTU"], profile.Addresses)
	if err != nil {
		return Profile{}, err
	}
	if err := validateAllowedIPs(peer["AllowedIPs"]); err != nil {
		return Profile{}, err
	}
	profile.Endpoint, err = endpoint(peer["Endpoint"])
	if err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func readMTU(value string, ips []netip.Addr) (int, error) {
	if value == "" {
		return 1280, nil
	}
	minimum := 576
	if slices.ContainsFunc(ips, netip.Addr.Is6) {
		minimum = 1280
	}
	mtu, err := strconv.Atoi(value)
	if err != nil || mtu < minimum || mtu > 1500 {
		return 0, errors.New("invalid AmneziaWG MTU")
	}
	return mtu, nil
}

func validateAllowedIPs(value string) error {
	for prefix := range strings.SplitSeq(value, ",") {
		if _, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err != nil {
			return errors.New("invalid AmneziaWG AllowedIPs")
		}
	}
	return nil
}

func addresses(value string, allowPrefix bool) ([]netip.Addr, error) {
	var result []netip.Addr
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		addr, err := netip.ParseAddr(item)
		if err != nil && allowPrefix {
			var prefix netip.Prefix
			prefix, err = netip.ParsePrefix(item)
			addr = prefix.Addr()
		}
		if err != nil || !addr.IsGlobalUnicast() || addr.Zone() != "" {
			return nil, errors.New("expected a unicast IP address")
		}
		if !slices.Contains(result, addr.Unmap()) {
			result = append(result, addr.Unmap())
		}
	}
	return result, nil
}

func endpoint(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	number, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || portErr != nil || number == 0 || !validHost(host) {
		return "", errors.New("invalid AmneziaWG Endpoint")
	}
	return net.JoinHostPort(host, strconv.FormatUint(number, 10)), nil
}

func validHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == "" && (addr.IsGlobalUnicast() || addr.IsLoopback())
	}
	host = strings.TrimSuffix(host, ".")
	if len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !endpointLabel.MatchString(label) {
			return false
		}
	}
	return true
}

var endpointLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
