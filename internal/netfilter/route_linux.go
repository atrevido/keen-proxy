//go:build linux

package netfilter

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// RulePriority places our rule before main (32766) but after Keenetic system
// rules (priorities 0 and 1158 on Netcraze Titan SE).
const RulePriority = 10100

// TProxyRoute installs a fwmark policy rule and a local default route via lo.
// These deliver marked TPROXY packets to the sing-box socket.
type TProxyRoute struct {
	Mark  uint32
	Table int
}

func (r TProxyRoute) rule() *netlink.Rule {
	rule := netlink.NewRule()
	rule.Family = unix.AF_INET
	rule.Mark = r.Mark
	mask := r.Mark
	rule.Mask = &mask
	rule.Table = r.Table
	rule.Priority = RulePriority
	return rule
}

func (r TProxyRoute) route() (*netlink.Route, error) {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return nil, fmt.Errorf("lo interface: %w", err)
	}
	_, def, _ := net.ParseCIDR("0.0.0.0/0")
	return &netlink.Route{
		LinkIndex: lo.Attrs().Index, Dst: def, Table: r.Table,
		Type: unix.RTN_LOCAL, Scope: netlink.SCOPE_HOST,
	}, nil
}

// Apply installs the policy rule and local route idempotently.
func (r TProxyRoute) Apply() error {
	rt, err := r.route()
	if err != nil {
		return err
	}
	if err := netlink.RouteReplace(rt); err != nil {
		return fmt.Errorf("local default route in table %d: %w", r.Table, err)
	}
	if err := netlink.RuleAdd(r.rule()); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("ip rule fwmark 0x%x: %w", r.Mark, err)
	}
	return nil
}

// Remove deletes every copy of the policy rule and removes the local route.
func (r TProxyRoute) Remove() error {
	for netlink.RuleDel(r.rule()) == nil {
	}
	rt, err := r.route()
	if err != nil {
		return err
	}
	if err := netlink.RouteDel(rt); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}
