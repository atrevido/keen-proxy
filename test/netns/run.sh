#!/bin/sh
# Run real interception tests inside isolated network namespaces.
# make integration supplies /work/keenproxy in a privileged disposable container.
#
#   client (192.168.77.10) --br0-- router (this namespace) --veth-- wan (198.51.100.2)
#
# There is no router NAT: successful client HTTP requires the embedded engine
# to carry the connection and return replies from the synthetic WAN.
set -eu

KEENPROXY=/work/keenproxy
fail() { echo "FAIL: $*"; exit 1; }
ok() { echo "ok   $*"; }

update-alternatives --set iptables /usr/sbin/iptables-legacy >/dev/null 2>&1 || true
echo 1 >/proc/sys/net/ipv4/ip_forward

# --- Isolated LAN/router/WAN topology
ip netns add client
ip netns add wan
ip link add br0 type bridge && ip addr add 192.168.77.1/24 dev br0 && ip link set br0 up
ip link add veth-c type veth peer name eth0 netns client
ip link set veth-c master br0 up
ip -n client addr add 192.168.77.10/24 dev eth0
ip -n client link set eth0 up && ip -n client link set lo up
ip -n client route add default via 192.168.77.1
ip link add veth-w type veth peer name eth0 netns wan
ip addr add 198.51.100.1/24 dev veth-w && ip link set veth-w up
ip -n wan addr add 198.51.100.2/24 dev eth0
ip -n wan addr add 198.51.100.3/24 dev eth0 # ordinary site; .2 hosts DNS and tunnel servers
ip -n wan addr add 198.51.100.4/24 dev eth0 # Steam: reachable directly, but mapped to Finland by GeoIP
ip -n wan addr add 198.51.100.5/24 dev eth0 # DNS fallback: RU, direct without a rule
ip -n wan link set eth0 up && ip -n wan link set lo up

# --- Synthetic internet: controlled DNS and HTTP targets
ip netns exec wan dnsmasq -k --no-resolv --no-hosts --listen-address=198.51.100.2 --bind-interfaces \
	--local-ttl=5 --address=/fallback.test/198.51.100.5 --address=/example.test/198.51.100.2 --address=/ads.test/198.51.100.2 --log-facility=/tmp/dnsmasq.log &
ip netns exec wan python3 -u -m http.server 80 --bind 198.51.100.3 >/tmp/http.log 2>&1 &
ip netns exec wan python3 -u -m http.server 80 --bind 198.51.100.4 >/tmp/steam.log 2>&1 &
ip netns exec wan python3 -u -m http.server 80 --bind 198.51.100.5 >/tmp/fallback-http.log 2>&1 &
# Hidden targets exist only inside the WAN namespace and have no router route.
# Reaching them proves use of the configured tunnel server.
ip -n wan addr add 203.0.113.80/32 dev lo
ip -n wan addr add 203.0.113.81/32 dev lo
ip netns exec wan python3 -u -m http.server 80 --bind 203.0.113.80 >/tmp/hidden.log 2>&1 &
ip netns exec wan python3 -u -m http.server 80 --bind 203.0.113.81 >/tmp/hidden2.log 2>&1 &
cat >/tmp/server.json <<'JSON'
{
  "log": {"level": "info"},
  "inbounds": [
    {"type": "shadowsocks", "listen": "198.51.100.2", "listen_port": 8388, "method": "aes-128-gcm", "password": "test-pass"},
    {"type": "vless", "listen": "198.51.100.2", "listen_port": 8443, "users": [{"uuid": "11111111-2222-3333-4444-555555555555"}]}
  ],
  "outbounds": [{"type": "direct"}]
}
JSON
ip netns exec wan /work/sbserver /tmp/server.json >/tmp/sbserver.log 2>&1 &
# The primary server starts offline to exercise failover before recovery.
cat >/tmp/server-main.json <<'JSON'
{
  "log": {"level": "info"},
  "inbounds": [{"type": "shadowsocks", "listen": "198.51.100.2", "listen_port": 8389, "method": "aes-128-gcm", "password": "test-pass"}],
  "outbounds": [{"type": "direct"}]
}
JSON
sleep 1

# --- Establish the negative control before enabling interception
if ip netns exec client curl -s -m 3 -o /dev/null http://198.51.100.3/; then
	fail "HTTP must fail without keenProxy (no NAT)"
fi
ok "client HTTP fails without keenProxy"

/work/mkgeo /tmp/geo.mmdb 198.51.100.0/24=RU 198.51.100.4/32=FI 203.0.113.0/24=FI
SS="ss://$(printf 'aes-128-gcm:test-pass' | base64 | tr -d '=\n')@198.51.100.2:8388"
SS_MAIN="ss://$(printf 'aes-128-gcm:test-pass' | base64 | tr -d '=\n')@198.51.100.2:8389"
cat >/tmp/config.yaml <<YAML
lan: {interface: br0}
dns: {listen: "0.0.0.0:5354", upstreams: ["udp://198.51.100.2:53"], fallback: "127.0.0.1:53"}
intercept: {mode: redirect}
geoip: {file: /tmp/geo.mmdb}
connections:
  - {id: ss, type: link, link: "$SS"}
  - {id: vless, type: link, link: "vless://11111111-2222-3333-4444-555555555555@198.51.100.2:8443?type=tcp"}
  - {id: main, name: Main, type: link, link: "$SS_MAIN", fallback: vless}
failover: {url: "http://203.0.113.80/", interval: 1, down_after: 2, up_after: 3}
groups:
  - {name: Advertising, target: block, entries: ["ads.test"]}
  - {name: All except Russia, target: ss, countries: [RU], except: true}
  - {name: Via VLESS, target: vless, entries: ["hidden.test"]}
  - {name: Steam direct, target: direct, entries: ["steam.test"]}
  - {name: With fallback, target: main, entries: ["backup.test"]}
networks:
  - {name: LAN, cidr: 192.168.77.0/24, mode: split}
YAML

$KEENPROXY -config /tmp/config.yaml -rule-sets /tmp/rule-sets -watch 1s \
	-iptables /usr/sbin/iptables -iptables-restore /usr/sbin/iptables-restore >/tmp/keenproxy.log 2>&1 &
KPID=$!
for i in $(seq 1 50); do
	iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null && break
	kill -0 $KPID 2>/dev/null || { cat /tmp/keenproxy.log; fail "keenProxy exited"; }
	sleep 0.2
done
iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null || { cat /tmp/keenproxy.log; fail "interception rules were not installed"; }
ok "keenProxy started with interception rules installed"

# --- Intercept UDP/TCP DNS to any destination and block a selected domain
a=$(ip netns exec client dig +short +time=2 +tries=1 @8.8.8.8 example.test A)
[ "$a" = "198.51.100.2" ] || fail "DNS UDP: '$a'"
ok "UDP DNS intercepted (our frontend answered a query to 8.8.8.8)"
a=$(ip netns exec client dig +tcp +short +time=2 +tries=1 @8.8.8.8 example.test A)
[ "$a" = "198.51.100.2" ] || fail "DNS TCP: '$a'"
ok "TCP DNS intercepted"
a=$(ip netns exec client dig +short +time=2 +tries=1 @192.168.77.1 x.ads.test A)
[ "$a" = "0.0.0.0" ] || fail "blocking: '$a'"
ok "blocked domain → 0.0.0.0"

# --- Prove TCP forwarding through sing-box
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.3/ || { cat /tmp/keenproxy.log; fail "HTTP through keenProxy"; }
grep -q '^198.51.100.1 ' /tmp/http.log || fail "server must see the router address: $(cat /tmp/http.log)"
ok "client HTTP passed through sing-box (server sees 198.51.100.1)"

# --- Hidden targets require the tunnel server in the WAN namespace
ip netns exec client curl -s -m 5 -o /dev/null http://203.0.113.80/ || { cat /tmp/keenproxy.log /tmp/sbserver.log; fail "Shadowsocks: restricted site is unavailable"; }
grep -q 'inbound/shadowsocks.*203.0.113.80' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "connection did not arrive through Shadowsocks"; }
ok "all-except-Russia group routed the restricted site through Shadowsocks"
# Sniffed HTTP Host selects VLESS; address .81 is absent from explicit groups.
ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: hidden.test' http://203.0.113.81/ || { cat /tmp/keenproxy.log /tmp/sbserver.log; fail "domain-based VLESS route"; }
grep -q 'inbound/vless.*203.0.113.81' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "connection did not arrive through VLESS"; }
ok "explicit domain wins over country: hidden.test used VLESS instead of Shadowsocks"
# Explicit domain routing precedes country rules within a routed policy.
# The same address without a hostname takes the country-selected tunnel.
ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: steam.test' http://198.51.100.4/ || { cat /tmp/keenproxy.log; fail "Steam direct"; }
grep -q '^198.51.100.1 ' /tmp/steam.log || { cat /tmp/steam.log; fail "Steam must connect directly (server sees the router)"; }
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.4/ || { cat /tmp/keenproxy.log; fail "Finnish address without a domain"; }
grep -q 'inbound/shadowsocks.*198.51.100.4' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "Finnish address without a domain must use Shadowsocks"; }
ok "outside-Russia tunnel with Steam direct: steam.test used direct, the same IP without a domain used Shadowsocks"

# --- The live tracker must account for bytes on each selected outbound
TOKEN=$(cat /opt/etc/keenproxy/token)
STATS=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8086/api/stats)
echo "$STATS" | python3 -c '
import json, sys
s = json.load(sys.stdin)
o = {x["tag"]: x for x in s["outbounds"] or []}
assert o.get("ss", {}).get("down_total", 0) > 0, o.get("ss")
assert o.get("vless", {}).get("down_total", 0) > 0, o.get("vless")
assert o.get("direct", {}).get("down_total", 0) > 0, o.get("direct")
' || { echo "$STATS"; fail "statistics did not count tunnel traffic"; }
HOSTS=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/stats/hosts?tag=vless")
echo "$HOSTS" | grep -q '203.0.113.81\|hidden.test' || { echo "$HOSTS"; fail "host is missing from the VLESS traffic table"; }
ok "statistics API counted ss/vless/direct bytes and listed the VLESS host"

# --- Read process counters after two procfs samples.
for attempt in $(seq 1 12); do
    STATS=$(curl -fsS -m 5 -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8086/api/stats)
    if echo "$STATS" | python3 -c '
import json, sys
process = json.load(sys.stdin)["system"].get("process", {})
sys.exit(not (process.get("rss_bytes", 0) > 0 and 0 <= process.get("cpu", -1) <= 100))
'; then
        break
    fi
    [ "$attempt" -lt 12 ] || fail "KeenProxy CPU or RSS is missing from live statistics"
    sleep 1
done
ok "statistics API reported live KeenProxy CPU and RSS"

# --- Verify route explanations, audit events and on-disk journal export
ROUTES=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/routes?limit=200")
echo "$ROUTES" | python3 -c '
import json, sys
e = json.load(sys.stdin)["entries"]
def find(host, out):
    for x in e:
        if x["host"] == host and x["outbound"] == out:
            return x
    raise SystemExit("missing record %s → %s: %s" % (host, out, [(x["host"], x["outbound"], x["rule"]) for x in e]))
assert find("steam.test", "direct")["rule"] == "network «LAN»: group «Steam direct»", find("steam.test", "direct")
assert find("hidden.test", "vless")["rule"] == "network «LAN»: group «Via VLESS»", find("hidden.test", "vless")
ss = find("198.51.100.4", "ss")
assert ss["rule"] == "network «LAN»: group «All except Russia» (all except RU)", ss
assert ss["client"].startswith("192.168.77.10:"), ss
' || { echo "$ROUTES" | head -c 3000; fail "routing journal"; }
AUDIT=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/audit")
echo "$AUDIT" | grep -q 'Start keenProxy' || { echo "$AUDIT"; fail "startup audit record is missing"; }
[ -s /tmp/journal/routes.jsonl ] && [ -s /tmp/journal/audit.jsonl ] || { ls -la /tmp/journal; fail "journals are not written to disk"; }
curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/routes/export?since=1h" | grep -q 'steam.test.*Steam direct' || fail "journal export"
ok "journal: Steam direct and country routes, startup audit, files and export"

# --- DNS fallback: preview is isolated; a live rule-set reload changes routing.
api_get() { curl -fsS -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/$1"; }
api_post() { curl -fsS -m 10 -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$2" "http://127.0.0.1:8086/api/$1"; }
api_post domains/resolve '{"names":["fallback.test"],"preview":true}' >/tmp/dns-preview.json
api_get domains | python3 -c 'import json,sys; assert not any(r["name"]=="fallback.test" for r in json.load(sys.stdin)["records"])' || fail "preview changed active cache"
api_get config | python3 -c 'import json,sys; c=json.load(sys.stdin);c["groups"].append({"name":"DNS reserve","target":"vless","entries":["fallback.test"]});print(json.dumps(c))' >/tmp/dns-config.json
curl -fsS -m 10 -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data-binary @/tmp/dns-config.json http://127.0.0.1:8086/api/config >/dev/null || fail "DNS group apply"
api_post domains/resolve '{"names":["fallback.test"]}' >/dev/null
dns_routed() {
 api_get 'journal/routes?limit=200' | python3 -c 'import json,sys; assert any(x.get("ip")=="198.51.100.5" and x["outbound"]=="vless" and "DNS" in x["rule"] for x in json.load(sys.stdin)["entries"])' 2>/dev/null
}
for i in $(seq 1 20); do
 ip netns exec client curl -s -m 2 --http1.0 -H 'Host:' http://198.51.100.5/ >/dev/null || true
 dns_routed && break
 sleep 0.1
done
dns_routed || { cat /tmp/keenproxy.log; fail "no-SNI DNS fallback"; }
ip netns exec client curl -fsS -m 3 -H 'Host: unrelated.test' http://198.51.100.5/ >/dev/null
api_get 'journal/routes?limit=200' | python3 -c 'import json,sys; assert any(x["host"]=="unrelated.test" and x["outbound"]=="direct" for x in json.load(sys.stdin)["entries"])' || fail "shared IP overrode Host"
for i in $(seq 1 35); do
 sleep 0.2
 if api_get domains | python3 -c 'import json,sys; r=[r for r in json.load(sys.stdin)["records"] if r["name"]=="fallback.test"]; assert r and not r[0]["addresses"]' 2>/dev/null; then break; fi
done
expired_direct() {
 ip netns exec client curl -fsS -m 3 --http1.0 -H 'Host:' http://198.51.100.5/ >/dev/null
 api_get 'journal/routes?limit=200' | python3 -c 'import json,sys; r=[x for x in json.load(sys.stdin)["entries"] if x.get("ip")=="198.51.100.5" and x.get("host")=="198.51.100.5"]; assert r and r[0]["outbound"]=="direct"' 2>/dev/null
}
for i in $(seq 1 15); do expired_direct && break; sleep 0.2; done
expired_direct || fail "expired DNS still routes"
ok "DNS reserve: preview isolated, no-SNI via VLESS, readable Host direct, TTL expiry removed route"

# --- Simulate firmware removing an interception jump; the watchdog repairs it
iptables -t nat -D PREROUTING -j KEENPROXY_TCP
for i in $(seq 1 25); do
	iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null && break
	sleep 0.2
done
iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null || fail "watchdog did not restore the removed jump"
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.3/ || fail "HTTP after recovery"
ok "watchdog restored the removed jump and HTTP works again"
curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/audit?actor=watchdog" | grep -q 'Interception rules restored' || fail "watchdog audit record is missing"
# A direct connection to a closed port must produce an engine error record.
ip netns exec client curl -s -m 3 -o /dev/null http://198.51.100.3:81/ || true
SYS=""
for i in $(seq 1 25); do
	SYS=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/system?source=sing-box")
	echo "$SYS" | grep -q '"source":"sing-box"' && break
	sleep 0.2
done
echo "$SYS" | grep -q '"source":"sing-box"' || { echo "$SYS"; tail -20 /tmp/keenproxy.log; fail "sing-box journal is not collected"; }
ok "audit records watchdog repairs and sing-box errors reach the system journal"

# --- Fail over from the offline primary, then recover after the up hysteresis
api() { curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086$1"; }
waitfor() { # API path, pattern, seconds
	for i in $(seq 1 $(($3 * 5))); do
		api "$1" | grep -q "$2" && return 0
		sleep 0.2
	done
	api "$1"; tail -20 /tmp/keenproxy.log; return 1
}
# Schedule a request and verify its host, actual outbound and route explanation.
route_is() {
	ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: backup.test' http://203.0.113.80/ || fail "backup.test is unavailable"
	api "/api/journal/routes?limit=20" | python3 -c '
import json, sys
out, note = sys.argv[1], sys.argv[2]
e = [x for x in json.load(sys.stdin)["entries"] if x["host"] == "backup.test"][0]
assert e["outbound"] == out and note in e["rule"], e
' "$1" "$2" || fail "backup.test must use $1"
}
waitfor "/api/journal/audit?target=main" 'Switch to fallback' 10 || fail "did not switch to fallback"
waitfor "/api/status" '"main":{"active":"vless"' 2 || fail "status does not show fallback"
route_is vless "fallback: «Main» is unavailable"
ok "primary unavailable: traffic uses fallback VLESS, confirmed by audit and routing journal"
ip netns exec wan /work/sbserver /tmp/server-main.json >/tmp/sbserver-main.log 2>&1 &
waitfor "/api/journal/audit?target=main" 'Return to primary' 15 || fail "did not return to primary"
route_is main "group «With fallback»"
grep -q 'inbound/shadowsocks.*203.0.113.80' /tmp/sbserver-main.log || fail "primary server did not receive the connection"
ok "primary recovered and traffic returned after up_after"

# --- DNS guard must also catch TCP already redirected into a local inbound.
ip netns exec wan python3 -u -m http.server 853 --bind 198.51.100.3 >/tmp/dot-guard.log 2>&1 &
sleep 1
ip netns exec client curl -fsS -m 3 http://198.51.100.3:853/ >/dev/null || fail "853 baseline"
api_get config | python3 -c 'import json,sys;c=json.load(sys.stdin);c["dns"]["block_encrypted"]=True;print(json.dumps(c))' >/tmp/guard-config.json
curl -fsS -m 10 -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data-binary @/tmp/guard-config.json http://127.0.0.1:8086/api/config >/dev/null || fail "guard apply"
if ip netns exec client curl -fsS -m 3 http://198.51.100.3:853/ >/dev/null 2>&1; then fail "DoT guard bypassed by TCP redirect"; fi
api_get config/previous | python3 -c 'import json,sys;assert not json.load(sys.stdin)["dns"].get("block_encrypted",False)' || fail "previous YAML backup"
dig +short +time=2 +tries=1 @127.0.0.1 -p 5354 example.test A | grep -q '198.51.100.2' || fail "guard blocked own DNS"
ok "DNS guard rejects redirected port 853, own resolver works, previous YAML available"

# --- Shutdown removes all interception owned by KeenProxy
kill -TERM $KPID
wait $KPID || true
if iptables-save | grep -q KEENPROXY; then
	iptables-save | grep KEENPROXY
	fail "interception rules remain after shutdown"
fi
ok "SIGTERM removed all KEENPROXY rules"
echo "PASS"
