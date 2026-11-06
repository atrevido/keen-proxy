package trusttunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// encoder builds links using the TrustTunnel export format.
type encoder struct{ b []byte }

func (e *encoder) varint(v uint64) {
	switch {
	case v < 1<<6:
		e.b = append(e.b, byte(v))
	case v < 1<<14:
		e.b = append(e.b, 0x40|byte(v>>8), byte(v))
	default:
		e.b = append(e.b, 0x80|byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
}

func (e *encoder) field(tag uint64, val []byte) {
	e.varint(tag)
	e.varint(uint64(len(val)))
	e.b = append(e.b, val...)
}

func (e *encoder) link() string { return "tt://?" + base64.RawURLEncoding.EncodeToString(e.b) }

func selfSigned(t *testing.T) []byte {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "vpn.example.com"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestParseLink(t *testing.T) {
	cert := selfSigned(t)
	var dns encoder
	for _, s := range []string{"8.8.8.8", "tls://1.1.1.1"} {
		dns.varint(uint64(len(s)))
		dns.b = append(dns.b, s...)
	}
	var e encoder
	e.field(0x00, []byte{2})
	e.field(0x01, []byte("vpn.example.com"))
	e.field(0x02, []byte("1.2.3.4:443"))
	e.field(0x02, []byte("[2001:db8::1]:443"))
	e.field(0x04, []byte{0})
	e.field(0x05, []byte("premium"))
	e.field(0x06, []byte(`s3cret"Pass`))
	e.field(0x08, append(append([]byte{}, cert...), cert...)) // Two-certificate chain.
	e.field(0x09, []byte{2})
	e.field(0x0C, []byte("Финляндия"))
	e.field(0x0D, dns.b)
	e.field(0x3F, []byte("неизвестное поле пропускается"))

	ep, err := ParseLink(e.link())
	if err != nil {
		t.Fatal(err)
	}
	if ep.Hostname != "vpn.example.com" || len(ep.Addresses) != 2 || ep.HasIPv6 || !ep.HTTP3 ||
		ep.Username != "premium" || ep.Password != `s3cret"Pass` || ep.Name != "Финляндия" || len(ep.DNSUpstreams) != 2 {
		t.Fatalf("разобрали не то: %+v", ep)
	}
	if n := strings.Count(ep.CertificatePEM, "BEGIN CERTIFICATE"); n != 2 {
		t.Errorf("ждали 2 сертификата в PEM, получили %d", n)
	}
	block, _ := pem.Decode([]byte(ep.CertificatePEM))
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Errorf("PEM не разбирается: %v", err)
	}

	toml := ClientConfig(ep, "127.0.0.1:17100")
	for _, want := range []string{
		`hostname = "vpn.example.com"`,
		`addresses = ["1.2.3.4:443", "[2001:db8::1]:443"]`,
		`password = "s3cret\"Pass"`,
		`upstream_protocol = "http3"`,
		`has_ipv6 = false`,
		"[listener.socks]\naddress = \"127.0.0.1:17100\"",
		`certificate = "-----BEGIN CERTIFICATE-----\n`,
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("в конфиге нет %q:\n%s", want, toml)
		}
	}
}

func TestParseLinkErrors(t *testing.T) {
	var noPass encoder
	noPass.field(0x01, []byte("h"))
	noPass.field(0x02, []byte("1.2.3.4:443"))
	noPass.field(0x05, []byte("u"))
	var future encoder
	future.field(0x00, []byte{9})
	for link, want := range map[string]string{
		"vless://x":   "tt://?",
		"tt://?***":   "base64",
		noPass.link(): "пароля",
		future.link(): "новее",
		"tt://?AQ":    "обрыв",
	} {
		if _, err := ParseLink(link); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, ждали %q", link, err, want)
		}
	}
}
