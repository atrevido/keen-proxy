// Command mkgeo writes a minimal deterministic country database for integration tests:
// mkgeo out.mmdb 198.51.100.0/24=RU 203.0.113.0/24=FI
package main

import (
	"log"
	"os"
	"strings"

	"github.com/atrevido/keenetic-proxy/internal/geoip/geoiptest"
)

// main maps CIDR=country arguments into the requested MMDB fixture.
func main() {
	nets := map[string]string{}
	for _, a := range os.Args[2:] {
		cidr, cc, _ := strings.Cut(a, "=")
		nets[cidr] = cc
	}
	if err := geoiptest.WriteTestDB(os.Args[1], nets); err != nil {
		log.Fatal(err)
	}
}
