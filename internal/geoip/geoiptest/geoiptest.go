package geoiptest

import (
	"net"
	"os"
	"slices"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// Package geoiptest creates small DB-IP-style MMDB fixtures for unit and
// integration tests. Prefixes are inserted shortest first so nested networks win.
// WriteTestDB writes CIDR-to-country fixtures with nested networks overriding parents.
func WriteTestDB(path string, nets map[string]string) error {
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: "DBIP-Country-Lite", RecordSize: 24, IncludeReservedNetworks: true})
	if err != nil {
		return err
	}
	var order []*net.IPNet
	codes := map[*net.IPNet]string{}
	for cidr, cc := range nets {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return err
		}
		order = append(order, n)
		codes[n] = cc
	}
	slices.SortFunc(order, func(a, b *net.IPNet) int {
		x, _ := a.Mask.Size()
		y, _ := b.Mask.Size()
		return x - y
	})
	for _, n := range order {
		if err := tree.Insert(n, mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String(codes[n])}}); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = tree.WriteTo(f)
	return err
}
