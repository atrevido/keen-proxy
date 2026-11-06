// Command nlprobe checks router netlink features using isolated temporary objects.
// It exercises ipset, marked policy routes and TUN without attaching interception jumps.
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	setNet   = "keenproxyprobe_net"
	setIP    = "keenproxyprobe_ip"
	table    = 201
	prio     = 31337
	mark     = 0x7a7a0000
	markMask = 0xffff0000
	tunName  = "keenproxyprb0"
	chain    = "KEENPROXYPROBE"
)

var failed int

// check reports one capability result and counts failures for the final exit status.
func check(name string, err error) bool {
	if err != nil {
		failed++
		fmt.Printf("[FAIL] %s :: %v\n", name, err)
		return false
	}
	fmt.Printf("[OK]   %s\n", name)
	return true
}

func u32(v uint32) *uint32 { return &v }

// main probes isolated kernel objects and defers cleanup before any capability checks.
func main() {
	defer cleanup()
	cleanup()

	fmt.Println("-- ipset through netlink")
	for _, s := range []struct{ name, typ string }{{setNet, "hash:net"}, {setIP, "hash:ip"}} {
		if check("create "+s.typ+" timeout", netlink.IpsetCreate(s.name, s.typ, netlink.IpsetCreateOptions{
			Timeout: u32(60), Family: unix.NFPROTO_IPV4,
		})) {
			e := &netlink.IPSetEntry{IP: net.ParseIP("203.0.113.0").To4(), Timeout: u32(30)}
			if s.typ == "hash:net" {
				e.CIDR = 24
			} else {
				e.IP = net.ParseIP("203.0.113.7").To4()
			}
			check("add "+s.typ+" with timeout", netlink.IpsetAdd(s.name, e))
			if r, err := netlink.IpsetList(s.name); check("list "+s.typ, err) {
				fmt.Printf("       entries=%d type=%s\n", len(r.Entries), r.TypeName)
			}
		}
	}
	out, err := exec.Command("iptables", "-w", "-t", "mangle", "-N", chain).CombinedOutput()
	if check("iptables -N", errOut(out, err)) {
		out, err = exec.Command("iptables", "-w", "-t", "mangle", "-A", chain, "-m", "set", "--match-set", setNet, "dst", "-j", "RETURN").CombinedOutput()
		check("iptables -m set --match-set (netlink-created set)", errOut(out, err))
		out, err = exec.Command("iptables", "-w", "-t", "mangle", "-A", chain, "-p", "udp", "-m", "multiport", "--dports", "443,8443", "-d", "203.0.113.1/32", "-j", "RETURN").CombinedOutput()
		check("iptables -m multiport", errOut(out, err))
	}

	fmt.Println("-- ip rule / route through netlink")
	r := netlink.NewRule()
	r.Family = unix.AF_INET
	r.Mark = mark
	r.Mask = u32(markMask)
	r.Table = table
	r.Priority = prio
	check("rule add fwmark/mask → table 201", netlink.RuleAdd(r))

	lo, err := netlink.LinkByName("lo")
	if check("link lo", err) {
		_, def, _ := net.ParseCIDR("0.0.0.0/0")
		check("route add local default dev lo table 201", netlink.RouteAdd(&netlink.Route{
			LinkIndex: lo.Attrs().Index, Dst: def, Table: table, Type: unix.RTN_LOCAL, Scope: netlink.SCOPE_HOST,
		}))
	}

	rules, err := netlink.RuleList(unix.AF_INET)
	if check("rule list", err) {
		for _, x := range rules {
			fmt.Printf("       prio=%d table=%d mark=%#x src=%v iif=%s\n", x.Priority, x.Table, x.Mark, x.Src, x.IifName)
		}
	}

	fmt.Println("-- TUN")
	f, err := openTun(tunName)
	if check("open /dev/net/tun + TUNSETIFF", err) {
		l, err := netlink.LinkByName(tunName)
		if check("TUN visible through netlink", err) {
			check("tun up", netlink.LinkSetUp(l))
		}
		f.Close()
	}

	if failed > 0 {
		fmt.Printf("\nSUMMARY: %d checks failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("\nSUMMARY: all checks passed")
}

// openTun requests a non-persistent TUN device through the kernel ioctl interface.
func openTun(name string) (*os.File, error) {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	var req [unix.IFNAMSIZ + 64]byte
	copy(req[:], name)
	*(*uint16)(unsafe.Pointer(&req[unix.IFNAMSIZ])) = unix.IFF_TUN | unix.IFF_NO_PI
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.TUNSETIFF, uintptr(unsafe.Pointer(&req[0]))); e != 0 {
		f.Close()
		return nil, e
	}
	return f, nil
}

// errOut retains command diagnostics when a capability check fails.
func errOut(out []byte, err error) error {
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanup removes only objects reserved by this probe, including after partial failure.
func cleanup() {
	exec.Command("iptables", "-w", "-t", "mangle", "-F", chain).Run()
	exec.Command("iptables", "-w", "-t", "mangle", "-X", chain).Run()
	netlink.IpsetDestroy(setNet)
	netlink.IpsetDestroy(setIP)
	r := netlink.NewRule()
	r.Family = unix.AF_INET
	r.Mark = mark
	r.Mask = u32(markMask)
	r.Table = table
	r.Priority = prio
	netlink.RuleDel(r)
	if routes, err := netlink.RouteListFiltered(unix.AF_INET, &netlink.Route{Table: table}, netlink.RT_FILTER_TABLE); err == nil {
		for _, rt := range routes {
			netlink.RouteDel(&rt)
		}
	}
	if l, err := netlink.LinkByName(tunName); err == nil {
		netlink.LinkDel(l)
	}
}
