#!/bin/sh
# ------------------------------------------------------------------------------
#  KeenProxy probe — inspect router kernel and firmware capabilities
#
#  Read-only by default; active checks require an explicit --active flag.
#  Active checks create temporary probe-owned objects:
#    - Netfilter/TUN kernel modules may remain loaded after the probe;
#    - temporary ipsets and unattached iptables chains (no PREROUTING jumps,
#      so no traffic is routed through them), followed by cleanup;
#    - a temporary policy rule/route using a reserved mark;
#    - a temporary TUN interface, followed by cleanup;
#    - a 30-second .254 alias on br0, skipped if already in use.
#
#  Best-effort redaction removes credentials, MAC addresses and public IPv4; review before sharing.
#
#  Run on the router under Entware:
#    sh probe.sh                 # read-only
#    sh probe.sh --active        # + active checks
#    sh probe.sh --out /opt/tmp/probe.txt
# ------------------------------------------------------------------------------

PROBE_VERSION=1
ACTIVE=no
OUT=""

while [ $# -gt 0 ]; do
	case "$1" in
		--active) ACTIVE=yes ;;
		--out) shift; OUT="$1" ;;
		-h|--help) sed -n '2,22p' "$0"; exit 0 ;;
		*) echo "Unknown option: $1" >&2; exit 2 ;;
	esac
	shift
done

[ -z "${OUT}" ] && OUT="/opt/tmp/keenproxy-probe-$(date +%Y%m%d-%H%M%S).txt"
mkdir -p "$(dirname "${OUT}")" 2>/dev/null

PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

# Accumulate capability facts for the final summary.
F_TPROXY=unknown; F_SOCKET=unknown; F_SET=unknown; F_TUN=unknown
F_REDIRECT=unknown; F_LOCAL_ROUTE=unknown; F_ALIAS=unknown; F_IPSET_TIMEOUT=unknown
F_IPV6=unknown; F_ARCH=unknown; F_MEM_MB=unknown; F_FW=unknown
FACTS=/tmp/keenproxy-probe-facts.$$

have() { command -v "$1" >/dev/null 2>&1; }
fact() { echo "$1=$2" >> "${FACTS}"; }

sec() { printf '\n==================== %s ====================\n' "$*"; }

# run "description" command... reports output and status without aborting the probe.
run() {
	local title="$1"; shift
	printf '\n--- %s\n$ %s\n' "${title}" "$*"
	"$@" 2>&1
	printf '[rc=%s]\n' "$?"
}

rci() {
	if have curl; then
		curl -s -m 5 "http://127.0.0.1:79/rci/$1"
	elif have wget; then
		wget -qO- -T 5 "http://127.0.0.1:79/rci/$1"
	else
		echo "curl/wget unavailable"
	fi
}

ndm() {
	if have ndmc; then ndmc -c "$*" 2>&1; else echo "ndmc unavailable"; fi
}

# Redact likely secrets and device/public-network identifiers from the report.
redact() {
	grep -viE 'private-key|preshared|password|passwd|psk|secret|token|"key"' \
	| sed -E 's/([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}/xx:xx:xx:xx:xx:xx/g' \
	| awk '{
		line = $0; out = ""
		while (match(line, /[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+/)) {
			ip = substr(line, RSTART, RLENGTH)
			pre = substr(line, 1, RSTART - 1)
			line = substr(line, RSTART + RLENGTH)
			split(ip, o, ".")
			a = o[1] + 0; b = o[2] + 0
			priv = (a == 10 || a == 127 || a == 0 || a >= 224 || \
				(a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168) || \
				(a == 100 && b >= 64 && b <= 127) || (a == 169 && b == 254) || \
				(a == 198 && (b == 18 || b == 19)) || (a == 203 && b == 0))
			if (!priv) ip = "<pub>"
			out = out pre ip
		}
		print out line
	}'
}

# ------------------------------------------------------------------------------
probe_meta() {
	sec "META"
	echo "probe_version=${PROBE_VERSION}"
	echo "date=$(date)"
	echo "active=${ACTIVE}"
	echo "uid=$(id -u 2>/dev/null)"
}

probe_system() {
	sec "SYSTEM"
	run "kernel" uname -a
	run "cpu" sh -c "grep -iE 'model name|system type|cpu model|isa|features|bogomips|hardware|processor' /proc/cpuinfo | sort | uniq -c"
	run "memory" sh -c "head -n 8 /proc/meminfo"
	run "uptime" uptime
	run "filesystems" df -h /opt /tmp
	run "/opt mounts" sh -c "mount | grep -E ' /opt | /tmp '"
	run "opkg architectures" opkg print-architecture
	run "entware_release" cat /opt/etc/entware_release

	F_ARCH=$(uname -m)
	F_MEM_MB=$(awk '/^MemTotal/ {print int($2/1024)}' /proc/meminfo)
	fact arch "${F_ARCH}"; fact mem_mb "${F_MEM_MB}"
	fact mem_avail_mb "$(awk '/^MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
	fact opt_free_mb "$(df -m /opt 2>/dev/null | awk 'NR==2 {print $4}')"
}

probe_keenetic() {
	sec "KEENETIC"
	printf '\n--- show version (RCI)\n'; rci show/version; echo
	F_FW=$(rci show/version | sed -n 's/.*"release": *"\([^"]*\)".*/\1/p' | head -n 1)
	fact firmware "${F_FW}"
	fact model "$(rci show/version | sed -n 's/.*"model": *"\([^"]*\)".*/\1/p' | head -n 1)"

	printf '\n--- show system (RCI)\n'; rci show/system; echo
	printf '\n--- show rc ppe (hardware acceleration)\n'; rci show/rc/ppe; echo

	printf '\n--- interfaces (id, type, description, state, link, address)\n'
	if have jq; then
		rci show/interface | jq -r 'to_entries[] | [.key, .value.type, (.value.description // ""), (.value.state // ""), (.value.link // ""), (.value.address // "")] | @tsv'
	else
		rci show/interface | grep -E '"(id|type|description|state|link|address|interface-name)"'
	fi

	printf '\n--- running-config: Bridge, Wireguard, Proxy, DNS, DHCP pools\n'
	ndm "show running-config" | awk '
		/^interface (Bridge|Wireguard|Proxy|OpenVPN|Sstp|L2TP|PPTP|IKE)/ { blk = 1 }
		/^ip dhcp pool/ || /^dns-proxy/ || /^ip name-server/ || /^opkg/ { blk = 1 }
		blk { print }
		/^!/ { blk = 0 }
	'

	printf '\n--- AmneziaWG options in WireGuard configuration\n'
	ndm "show running-config" | grep -iE 'wireguard (asc|obfuscat)|asc ' || echo "no asc options configured (this does not prove missing firmware support)"
}

probe_netfilter() {
	sec "NETFILTER"
	run "iptables tables" cat /proc/net/ip_tables_names
	run "targets iptables" sh -c "sort -u /proc/net/ip_tables_targets"
	run "matches iptables" sh -c "sort -u /proc/net/ip_tables_matches"
	run "targets ip6tables" sh -c "sort -u /proc/net/ip6_tables_targets"

	run "relevant loaded modules" sh -c "grep -iE 'tproxy|socket|xt_set|ip_set|connmark|conntrack|tun|nat|redirect|wireguard|amnezia|nf_|xt_' /proc/modules | awk '{print \$1, \$2, \$3}'"
	run "relevant module files" sh -c "find /lib/modules/\$(uname -r) -name '*.ko' 2>/dev/null | grep -iE 'tproxy|socket|xt_set|ip_set|connmark|tun|wireguard|amnezia|xt_mac|multiport|addrtype|nft' | sed 's|.*/||' | sort"

	printf '\n--- executables\n'
	for b in iptables iptables-restore iptables-save ip6tables ipset ip conntrack nft ndmc curl wget jq kdig dig nc socat modprobe insmod; do
		p=$(command -v "$b" 2>/dev/null)
		printf '%-18s %s\n' "$b" "${p:-—}"
	done
	run "iptables -V" iptables -V
	run "ipset -v" ipset -v
	run "ip -V" ip -V

	run "nat PREROUTING" sh -c "iptables -t nat -S PREROUTING | head -n 40"
	run "mangle PREROUTING" sh -c "iptables -t mangle -S PREROUTING | head -n 40"
	run "rule count per table" sh -c "for t in filter nat mangle raw; do printf '%s %s\n' \$t \"\$(iptables -t \$t -S 2>/dev/null | wc -l)\"; done"
	run "existing ipsets" sh -c "ipset -n list"

	grep -q '^TPROXY$' /proc/net/ip_tables_targets 2>/dev/null && F_TPROXY=yes
	grep -q '^REDIRECT$' /proc/net/ip_tables_targets 2>/dev/null && F_REDIRECT=yes
	grep -q '^socket$' /proc/net/ip_tables_matches 2>/dev/null && F_SOCKET=yes
	grep -q '^set$' /proc/net/ip_tables_matches 2>/dev/null && F_SET=yes
	fact tproxy_target_registered "${F_TPROXY}"
	fact redirect_target_registered "${F_REDIRECT}"
	fact socket_match_registered "${F_SOCKET}"
	fact set_match_registered "${F_SET}"
	fact tproxy_ko_on_disk "$(find /lib/modules/$(uname -r) -iname '*TPROXY*.ko' 2>/dev/null | grep -q . && echo yes || echo no)"
}

probe_routing() {
	sec "ROUTING"
	run "ip rule" ip -4 rule list
	run "main table" ip -4 route show table main
	run "populated route tables" sh -c "ip -4 route show table all 2>/dev/null | sed -n 's/.* table \([^ ]*\).*/\1/p' | sort | uniq -c"
	run "IPv4 addresses" ip -4 addr show
	run "br0" ip addr show br0
}

probe_misc() {
	sec "TUN / IPv6 / SYSCTL / PORTS"
	run "/dev/net/tun" ls -l /dev/net/tun
	[ -c /dev/net/tun ] && F_TUN=yes || F_TUN=no
	fact dev_net_tun "${F_TUN}"

	n6=$(wc -l < /proc/net/if_inet6 2>/dev/null)
	echo "if_inet6 rows: ${n6:-0}"
	[ "${n6:-0}" -gt 0 ] && F_IPV6=yes || F_IPV6=no
	fact ipv6 "${F_IPV6}"

	printf '\n--- sysctl\n'
	for k in net/ipv4/ip_forward net/ipv4/conf/all/route_localnet net/ipv4/conf/br0/route_localnet \
		net/ipv4/conf/all/rp_filter net/ipv4/conf/br0/rp_filter net/netfilter/nf_conntrack_max \
		net/netfilter/nf_conntrack_count net/core/rmem_max net/core/wmem_max net/ipv4/tcp_congestion_control \
		net/ipv4/tcp_available_congestion_control fs/file-max; do
		printf '%-45s %s\n' "$k" "$(cat /proc/sys/$k 2>/dev/null || echo —)"
	done

	run "listening sockets" sh -c "netstat -lntu 2>/dev/null || ss -lntu"
	printf '\n--- reserved port listeners\n'
	for p in 53 79 80 443 8085 9753 1080; do
		printf 'port %-5s %s\n' "$p" "$(netstat -lntu 2>/dev/null | awk -v p=":$p\$" '$4 ~ p {print $1, $4}' | tr '\n' ' ')"
	done

	sec "INSTALLED SOFTWARE"
	run "kvas/kvaspro/sing-box/xray/hysteria/dnsmasq" sh -c "ls -d /opt/apps/kvas /opt/apps/kvaspro /opt/sbin/xray /opt/bin/sing-box /opt/sbin/sing-box /opt/apps/kvas/hysteria 2>/dev/null; pidof dnsmasq sing-box xray hysteria AdGuardHome 2>/dev/null"
	run "process memory overview" sh -c "ps w 2>/dev/null | head -n 1; ps w 2>/dev/null | sed 1d | sort -k3 -n -r | head -n 15"
}

# ------------------------------------------------------------------------------
# Active checks
# ------------------------------------------------------------------------------
P_CHAIN=KEENPROXYPROBE
P_SET=keenproxyprobe
# BusyBox ip accepts numeric table IDs only up to 255.
P_TABLE=201
P_MARK=0x7a7a0000/0xffff0000
P_TUN=keenproxyprb0
P_ALIAS=""

cleanup() {
	iptables -t mangle -F "${P_CHAIN}" 2>/dev/null; iptables -t mangle -X "${P_CHAIN}" 2>/dev/null
	iptables -t nat -F "${P_CHAIN}" 2>/dev/null; iptables -t nat -X "${P_CHAIN}" 2>/dev/null
	ip6tables -t filter -F "${P_CHAIN}" 2>/dev/null; ip6tables -t filter -X "${P_CHAIN}" 2>/dev/null
	ipset destroy "${P_SET}" 2>/dev/null
	ip rule del fwmark "${P_MARK}" table "${P_TABLE}" 2>/dev/null
	ip route flush table "${P_TABLE}" 2>/dev/null
	ip link del "${P_TUN}" 2>/dev/null
	[ -n "${P_ALIAS}" ] && ip addr del "${P_ALIAS}" dev br0 2>/dev/null
	P_ALIAS=""
}

try() {
	# try "description" command... records a non-fatal capability result.
	local title="$1"; shift
	local out
	out=$("$@" 2>&1)
	if [ $? -eq 0 ]; then
		printf '[OK]   %s\n' "${title}"; return 0
	else
		printf '[FAIL] %s :: %s\n' "${title}" "$(echo "${out}" | head -n 2 | tr '\n' ' ')"; return 1
	fi
}

load_module() {
	local m="$1" ko
	grep -q "^$m " /proc/modules && { printf '[OK]   module %s already loaded\n' "$m"; return 0; }
	if have modprobe && modprobe "$m" >/dev/null 2>&1; then
		printf '[OK]   modprobe %s\n' "$m"; return 0
	fi
	ko=$(find /lib/modules/$(uname -r) -name "$m.ko" 2>/dev/null | head -n 1)
	if [ -n "${ko}" ]; then
		try "insmod ${ko##*/}" insmod "${ko}"; return $?
	fi
	printf '[--]   module %s not found on disk\n' "$m"; return 1
}

probe_active() {
	sec "ACTIVE CHECKS"
	trap cleanup EXIT INT TERM
	cleanup

	echo "-- kernel modules"
	for m in nf_defrag_ipv6 nf_tproxy_core nf_tproxy_ipv4 nf_tproxy_ipv6 xt_TPROXY nf_socket_ipv4 nf_socket_ipv6 xt_socket ip_set ip_set_hash_net ip_set_hash_ip xt_set xt_connmark xt_mac xt_multiport xt_addrtype tun; do
		load_module "$m"
	done
	grep -q '^TPROXY$' /proc/net/ip_tables_targets && F_TPROXY=yes || F_TPROXY=no
	grep -q '^socket$' /proc/net/ip_tables_matches && F_SOCKET=yes || F_SOCKET=no

	echo "-- ipset (CLI may be absent; use tools/nlprobe for netlink checks)"
	have ipset || echo "[--]   ipset executable unavailable; run nlprobe"
	have ipset && try "ipset create hash:net timeout" ipset create "${P_SET}" hash:net family inet timeout 60 \
		&& F_IPSET_TIMEOUT=yes || F_IPSET_TIMEOUT=no
	have ipset && try "ipset add with timeout" ipset add "${P_SET}" 203.0.113.0/24 timeout 30
	have ipset && try "ipset list" ipset list "${P_SET}"

	echo "-- iptables mangle (unattached chain; no traffic interception)"
	try "mangle -N" iptables -t mangle -N "${P_CHAIN}"
	try "TPROXY udp" iptables -t mangle -A "${P_CHAIN}" -d 203.0.113.1/32 -p udp -j TPROXY --on-ip 127.0.0.1 --on-port 1 --tproxy-mark 0x1/0x1 && F_TPROXY=yes || F_TPROXY=no
	try "TPROXY tcp" iptables -t mangle -A "${P_CHAIN}" -d 203.0.113.1/32 -p tcp -j TPROXY --on-ip 127.0.0.1 --on-port 1 --tproxy-mark 0x1/0x1
	try "match socket --transparent" iptables -t mangle -A "${P_CHAIN}" -m socket --transparent -j RETURN && F_SOCKET=yes || F_SOCKET=no
	try "match set" iptables -t mangle -A "${P_CHAIN}" -m set --match-set "${P_SET}" dst -j RETURN && F_SET=yes || F_SET=no
	try "match mac" iptables -t mangle -A "${P_CHAIN}" -m mac --mac-source 02:00:00:00:00:01 -j RETURN
	try "CONNMARK restore" iptables -t mangle -A "${P_CHAIN}" -j CONNMARK --restore-mark
	try "MARK set" iptables -t mangle -A "${P_CHAIN}" -d 203.0.113.1/32 -j MARK --set-mark 0x7a7a0000/0xffff0000
	try "match conntrack ctstate" iptables -t mangle -A "${P_CHAIN}" -m conntrack --ctstate NEW -d 203.0.113.1/32 -j RETURN
	try "match multiport" iptables -t mangle -A "${P_CHAIN}" -p udp -m multiport --dports 443,8443 -d 203.0.113.1/32 -j RETURN

	echo "-- iptables nat"
	try "nat -N" iptables -t nat -N "${P_CHAIN}"
	try "REDIRECT tcp" iptables -t nat -A "${P_CHAIN}" -d 203.0.113.1/32 -p tcp -j REDIRECT --to-ports 1 && F_REDIRECT=yes || F_REDIRECT=no
	try "DNAT udp 53" iptables -t nat -A "${P_CHAIN}" -d 203.0.113.1/32 -p udp --dport 53 -j DNAT --to-destination 127.0.0.1:1
	try "iptables-restore --noflush" sh -c "printf '*nat\n-A ${P_CHAIN} -d 203.0.113.2/32 -p tcp -j RETURN\nCOMMIT\n' | iptables-restore --noflush"

	echo "-- ip6tables"
	try "ip6tables REJECT" sh -c "ip6tables -t filter -N ${P_CHAIN} && ip6tables -t filter -A ${P_CHAIN} -d 2001:db8::1/128 -j REJECT"

	echo "-- ip rule / route"
	try "ip rule fwmark/mask" ip rule add fwmark "${P_MARK}" table "${P_TABLE}" priority 31337
	if try "ip route local default dev lo" ip route add local default dev lo table "${P_TABLE}" \
		|| try "ip route local 0.0.0.0/0 dev lo" ip route add local 0.0.0.0/0 dev lo table "${P_TABLE}"; then
		F_LOCAL_ROUTE=yes
	else
		F_LOCAL_ROUTE=no
	fi

	echo "-- TUN"
	if [ -c /dev/net/tun ] && ! ip tuntap help >/dev/null 2>&1; then
		echo "[--]   BusyBox ip lacks tuntap; use nlprobe for TUN"; F_TUN=see_nlprobe
	elif [ -c /dev/net/tun ]; then
		try "ip tuntap add" ip tuntap add mode tun name "${P_TUN}" && F_TUN=yes || F_TUN=no
	else
		echo "[--]   /dev/net/tun is unavailable"; F_TUN=no
	fi

	echo "-- temporary .254 alias on br0 (30 seconds)"
	local cidr addr prefix cand
	cidr=$(ip -4 addr show br0 2>/dev/null | awk '/inet / {print $2; exit}')
	addr=${cidr%/*}; prefix=${cidr#*/}
	if [ -z "${cidr}" ]; then
		echo "[--]   br0 has no IPv4 address"
	else
		cand="${addr%.*}.254"
		if [ "${cand}" = "${addr}" ]; then
			echo "[--]   router already uses .254; check skipped"; F_ALIAS=skipped
		elif ping -c 1 -W 1 "${cand}" >/dev/null 2>&1 || grep -q "^${cand} " /proc/net/arp; then
			echo "[--]   ${cand} is already used on the LAN; check skipped"; F_ALIAS=busy
		elif try "ip addr add ${cand}/${prefix} dev br0" ip addr add "${cand}/${prefix}" dev br0 label br0:kp; then
			P_ALIAS="${cand}/${prefix}"
			F_ALIAS=kept
			i=0
			while [ $i -lt 6 ]; do
				sleep 5; i=$((i + 1))
				if ! ip -4 addr show br0 | grep -q "inet ${cand}/"; then
					echo "[WARN] ndm removed the alias after ~$((i * 5)) seconds"; F_ALIAS=removed_by_ndm; P_ALIAS=""; break
				fi
			done
			[ "${F_ALIAS}" = kept ] && echo "[OK]   alias remained for 30 seconds"
		else
			F_ALIAS=no
		fi
	fi

	cleanup
	trap - EXIT INT TERM
	echo "-- temporary objects removed"
	fact tproxy_works "${F_TPROXY}"; fact socket_works "${F_SOCKET}"; fact set_works "${F_SET}"
	fact redirect_works "${F_REDIRECT}"; fact ipset_timeout "${F_IPSET_TIMEOUT}"
	fact local_route "${F_LOCAL_ROUTE}"; fact tun_works "${F_TUN}"; fact alias_254 "${F_ALIAS}"
}

summary() {
	sec "SUMMARY"
	[ -f "${FACTS}" ] && sort -u "${FACTS}" | awk -F= '{a[$1]=$2} END {for (k in a) printf "%-28s %s\n", k, a[k]}' | sort
	echo
	# Suggest an interception mode from observed capabilities.
	if [ "${ACTIVE}" = yes ]; then
		if [ "${F_TPROXY}" = yes ] && [ "${F_LOCAL_ROUTE}" = yes ] && [ "${F_REDIRECT}" = yes ]; then
			echo "=> TCP REDIRECT and UDP TPROXY are available."
		elif [ "${F_REDIRECT}" = yes ]; then
			echo "=> Compatible mode only: TCP REDIRECT; UDP is not intercepted (QUIC is rejected)."
		else
			echo "=> REDIRECT unavailable; install the firmware Netfilter kernel modules component."
		fi
		[ "${F_TUN}" = yes ] && echo "=> TUN is available for compatible userspace clients."
		[ "${F_SET}" = yes ] && [ "${F_IPSET_TIMEOUT}" = yes ] && echo "=> ipset timeout works; a future destination fast-path may be feasible."
		case "${F_ALIAS}" in
			kept) echo "=> A .254 alias remains on br0; a separate management address may be feasible." ;;
			removed_by_ndm) echo "=> ndm removes unmanaged aliases; configure .254 through firmware or reapply via a hook." ;;
		esac
	else
		echo "=> Use --active to check TPROXY, ipset, TUN and the .254 alias."
	fi
	rm -f "${FACTS}"
}

main() {
	: > "${FACTS}"
	probe_meta
	probe_system
	probe_keenetic
	probe_netfilter
	probe_routing
	probe_misc
	[ "${ACTIVE}" = yes ] && probe_active
	summary
}

if [ "$(id -u 2>/dev/null)" != 0 ]; then
	echo "Root privileges are required on the Entware router." >&2
	exit 1
fi

main 2>&1 | redact | tee "${OUT}"
echo
echo "Report saved: ${OUT}"
