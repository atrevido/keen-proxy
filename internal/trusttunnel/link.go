// Package trusttunnel supports AdGuard's TrustTunnel through an external
// trusttunnel_client and a loopback SOCKS5 outbound used by sing-box.
//
// Links use tt://?<unpadded-base64url> around TLV data with QUIC-style tag
// and length integers (RFC 9000 §16), as documented in the upstream
// github.com/TrustTunnel/TrustTunnel DEEP_LINK.md specification.
package trusttunnel

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

const supportedVersion = 2

// Endpoint contains server parameters decoded from a TrustTunnel link.
type Endpoint struct {
	Version          uint64
	Hostname         string
	Addresses        []string // host:port
	CustomSNI        string
	HasIPv6          bool
	Username         string
	Password         string
	SkipVerification bool
	CertificatePEM   string // PEM certificate chain; empty uses system CAs.
	HTTP3            bool   // upstream_protocol = http3
	AntiDPI          bool
	ClientRandom     string
	Name             string
	DNSUpstreams     []string
	SubscriptionURL  string
}

// ParseLink decodes length-checked TLV fields from a base64url TrustTunnel link.
func ParseLink(link string) (Endpoint, error) {
	link = strings.TrimSpace(link)
	payload, ok := strings.CutPrefix(link, "tt://?")
	if !ok {
		return Endpoint{}, fmt.Errorf("ссылка TrustTunnel должна начинаться с tt://?")
	}
	if i := strings.IndexByte(payload, '#'); i >= 0 {
		payload = payload[:i]
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil {
		return Endpoint{}, fmt.Errorf("tt://: не base64url: %w", err)
	}
	e := Endpoint{HasIPv6: true}
	r := reader{b: raw}
	for !r.done() {
		tag, err := r.varint()
		if err != nil {
			return Endpoint{}, err
		}
		n, err := r.varint()
		if err != nil {
			return Endpoint{}, err
		}
		val, err := r.take(n)
		if err != nil {
			return Endpoint{}, fmt.Errorf("поле 0x%02x: %w", tag, err)
		}
		switch tag {
		case 0x00:
			e.Version, err = (&reader{b: val}).varint()
		case 0x01:
			e.Hostname = string(val)
		case 0x02:
			e.Addresses = append(e.Addresses, string(val))
		case 0x03:
			e.CustomSNI = string(val)
		case 0x04:
			e.HasIPv6, err = boolean(val)
		case 0x05:
			e.Username = string(val)
		case 0x06:
			e.Password = string(val)
		case 0x07:
			e.SkipVerification, err = boolean(val)
		case 0x08:
			e.CertificatePEM = derToPEM(val)
		case 0x09:
			var p uint64
			p, err = (&reader{b: val}).varint()
			e.HTTP3 = p == 2
		case 0x0A:
			e.AntiDPI, err = boolean(val)
		case 0x0B:
			e.ClientRandom = string(val)
		case 0x0C:
			e.Name = string(val)
		case 0x0D:
			e.DNSUpstreams, err = stringList(val)
		case 0x0E:
			e.SubscriptionURL = string(val)
		default:
			// Skip unknown tags as required by the specification.
		}
		if err != nil {
			return Endpoint{}, fmt.Errorf("поле 0x%02x: %w", tag, err)
		}
	}
	if e.Version > supportedVersion {
		return Endpoint{}, fmt.Errorf("версия ссылки %d новее поддерживаемой %d", e.Version, supportedVersion)
	}
	if e.SubscriptionURL != "" && e.Hostname == "" {
		return Endpoint{}, fmt.Errorf("ссылка-подписка без параметров сервера пока не поддерживается")
	}
	switch {
	case e.Hostname == "":
		return Endpoint{}, fmt.Errorf("в ссылке нет hostname")
	case len(e.Addresses) == 0:
		return Endpoint{}, fmt.Errorf("в ссылке нет адресов сервера")
	case e.Username == "" || e.Password == "":
		return Endpoint{}, fmt.Errorf("в ссылке нет логина или пароля")
	}
	return e, nil
}

// reader bounds reads within a decoded TrustTunnel TLV payload.
type reader struct {
	b   []byte
	off int
}

func (r *reader) done() bool { return r.off >= len(r.b) }

// varint decodes a QUIC integer whose first two bits encode its byte length.
func (r *reader) varint() (uint64, error) {
	if r.done() {
		return 0, fmt.Errorf("обрыв данных")
	}
	n := 1 << (r.b[r.off] >> 6)
	if r.off+n > len(r.b) {
		return 0, fmt.Errorf("обрыв переменного целого")
	}
	v := uint64(r.b[r.off] & 0x3f)
	for i := 1; i < n; i++ {
		v = v<<8 | uint64(r.b[r.off+i])
	}
	r.off += n
	return v, nil
}

func (r *reader) take(n uint64) ([]byte, error) {
	if uint64(len(r.b)-r.off) < n {
		return nil, fmt.Errorf("длина %d больше оставшихся данных", n)
	}
	v := r.b[r.off : r.off+int(n)]
	r.off += int(n)
	return v, nil
}

func boolean(v []byte) (bool, error) {
	if len(v) != 1 || v[0] > 1 {
		return false, fmt.Errorf("неверный bool")
	}
	return v[0] == 1, nil
}

// stringList decodes consecutive QUIC-length-prefixed strings and rejects truncated elements.
func stringList(v []byte) ([]string, error) {
	r := reader{b: v}
	var out []string
	for !r.done() {
		n, err := r.varint()
		if err != nil {
			return nil, err
		}
		s, err := r.take(n)
		if err != nil {
			return nil, err
		}
		out = append(out, string(s))
	}
	return out, nil
}

// derToPEM converts concatenated DER certificates into the client's PEM format.
func derToPEM(der []byte) string {
	var b strings.Builder
	for len(der) > 0 {
		n := derLength(der)
		if n <= 0 || n > len(der) {
			n = len(der)
		}
		pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: der[:n]})
		der = der[n:]
	}
	return b.String()
}

// derLength returns the full byte length of the first ASN.1 SEQUENCE.
func derLength(b []byte) int {
	if len(b) < 2 || b[0] != 0x30 {
		return -1
	}
	if b[1] < 0x80 {
		return 2 + int(b[1])
	}
	k := int(b[1] & 0x7f)
	if k == 0 || k > 4 || len(b) < 2+k {
		return -1
	}
	n := 0
	for i := 0; i < k; i++ {
		n = n<<8 | int(b[2+i])
	}
	return 2 + k + n
}
