package compile

import (
	"encoding/json"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

// ConnectionValidationConfig contains only profiles and a local resolver: no
// listeners, rule-set files, firewall, routes from imported configs or services.
func ConnectionValidationConfig(cfg *model.Config) ([]byte, error) {
	profiles, err := connections(cfg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(obj{
		"log":       obj{"disabled": true},
		"dns":       obj{"servers": []obj{{"type": "local", "tag": "validation-dns"}}},
		"route":     obj{"default_domain_resolver": "validation-dns"},
		"outbounds": profiles.outbounds, "endpoints": profiles.endpoints,
	})
}
