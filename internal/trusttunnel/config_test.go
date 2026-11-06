package trusttunnel

import (
	"strings"
	"testing"
)

func TestClientConfigPreservesCustomSNI(t *testing.T) {
	e := Endpoint{Hostname: "endpoint.example", CustomSNI: "front.example", Addresses: []string{"192.0.2.1:443"}, Username: "user", Password: "password"}
	got := ClientConfig(e, "127.0.0.1:17100")
	if !strings.Contains(got, "[endpoint]\nhostname = \"endpoint.example\"\ncustom_sni = \"front.example\"\n") {
		t.Fatalf("endpoint configuration lost custom SNI: %s", got)
	}
}
