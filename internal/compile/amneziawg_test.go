package compile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestAmneziaWGIsTCPOnlyAndSecretsStayOutOfEngine(t *testing.T) {
	cfg, err := model.Parse([]byte(`connections:
  - id: awg
    name: AWG
    type: amneziawg
    amneziawg:
      profile: |
        [Interface]
        PrivateKey = IBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
        Address = 198.18.0.3/32
        [Peer]
        PublicKey = EBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
        AllowedIPs = 0.0.0.0/0
        Endpoint = 192.0.2.10:51820
`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(Input{Config: cfg, RuleSetDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		Outbounds []struct {
			Tag     string
			Type    string
			Network string
			Server  string
			Port    uint16 `json:"server_port"`
		}
	}
	if err := json.Unmarshal(out.SingBox, &compiled); err != nil {
		t.Fatal(err)
	}
	for _, item := range compiled.Outbounds {
		if item.Tag == "awg" && item.Type == "socks" && item.Network == "tcp" && item.Server == "127.0.0.1" && item.Port != 0 && !strings.Contains(string(out.SingBox), "IBAQE") {
			return
		}
	}
	t.Fatal("expected local TCP-only outbound without AWG credentials")
}
