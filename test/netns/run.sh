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
ip -n wan addr add 198.51.100.3/24 dev eth0 # обычный сайт; .2 — DNS и туннельные серверы
ip -n wan addr add 198.51.100.4/24 dev eth0 # «Steam»: доступен напрямую, но по базе стран — Финляндия
ip -n wan addr add 198.51.100.5/24 dev eth0 # DNS fallback: RU, без правила — напрямую
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
	fail "без keenProxy HTTP не должен проходить (нет NAT)"
fi
ok "без keenProxy HTTP с клиента не проходит"

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
  - {id: main, name: Основное, type: link, link: "$SS_MAIN", fallback: vless}
failover: {url: "http://203.0.113.80/", interval: 1, down_after: 2, up_after: 3}
groups:
  - {name: Реклама, target: block, entries: ["ads.test"]}
  - {name: Всё кроме России, target: ss, countries: [RU], except: true}
  - {name: Через VLESS, target: vless, entries: ["hidden.test"]}
  - {name: Steam напрямую, target: direct, entries: ["steam.test"]}
  - {name: С резервным, target: main, entries: ["backup.test"]}
networks:
  - {name: LAN, cidr: 192.168.77.0/24, mode: split}
YAML

$KEENPROXY -config /tmp/config.yaml -rule-sets /tmp/rule-sets -watch 1s \
	-iptables /usr/sbin/iptables -iptables-restore /usr/sbin/iptables-restore >/tmp/keenproxy.log 2>&1 &
KPID=$!
for i in $(seq 1 50); do
	iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null && break
	kill -0 $KPID 2>/dev/null || { cat /tmp/keenproxy.log; fail "keenProxy завершился"; }
	sleep 0.2
done
iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null || { cat /tmp/keenproxy.log; fail "правила не поставлены"; }
ok "keenProxy запущен, правила на месте"

# --- Intercept UDP/TCP DNS to any destination and block a selected domain
a=$(ip netns exec client dig +short +time=2 +tries=1 @8.8.8.8 example.test A)
[ "$a" = "198.51.100.2" ] || fail "DNS UDP: '$a'"
ok "DNS UDP перехвачен (запрос к 8.8.8.8 ответил наш фронтенд)"
a=$(ip netns exec client dig +tcp +short +time=2 +tries=1 @8.8.8.8 example.test A)
[ "$a" = "198.51.100.2" ] || fail "DNS TCP: '$a'"
ok "DNS TCP перехвачен"
a=$(ip netns exec client dig +short +time=2 +tries=1 @192.168.77.1 x.ads.test A)
[ "$a" = "0.0.0.0" ] || fail "блокировка: '$a'"
ok "заблокированный домен → 0.0.0.0"

# --- Prove TCP forwarding through sing-box
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.3/ || { cat /tmp/keenproxy.log; fail "HTTP через keenProxy"; }
grep -q '^198.51.100.1 ' /tmp/http.log || fail "сервер должен видеть адрес роутера: $(cat /tmp/http.log)"
ok "HTTP с клиента прошёл через sing-box (сервер видит 198.51.100.1)"

# --- Hidden targets require the tunnel server in the WAN namespace
ip netns exec client curl -s -m 5 -o /dev/null http://203.0.113.80/ || { cat /tmp/keenproxy.log /tmp/sbserver.log; fail "Shadowsocks: закрытый сайт недоступен"; }
grep -q 'inbound/shadowsocks.*203.0.113.80' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "соединение пришло не через Shadowsocks"; }
ok "группа «всё, кроме России»: закрытый сайт ушёл в Shadowsocks"
# Sniffed HTTP Host selects VLESS; address .81 is absent from explicit groups.
ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: hidden.test' http://203.0.113.81/ || { cat /tmp/keenproxy.log /tmp/sbserver.log; fail "VLESS по домену"; }
grep -q 'inbound/vless.*203.0.113.81' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "соединение пришло не через VLESS"; }
ok "домен из белого списка важнее страны: hidden.test ушёл в VLESS, а не в Shadowsocks"
# Explicit domain routing precedes country rules within a routed policy.
# The same address without a hostname takes the country-selected tunnel.
ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: steam.test' http://198.51.100.4/ || { cat /tmp/keenproxy.log; fail "Steam напрямую"; }
grep -q '^198.51.100.1 ' /tmp/steam.log || { cat /tmp/steam.log; fail "Steam должен идти напрямую (сервер видит роутер)"; }
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.4/ || { cat /tmp/keenproxy.log; fail "финский адрес без домена"; }
grep -q 'inbound/shadowsocks.*198.51.100.4' /tmp/sbserver.log || { cat /tmp/sbserver.log; fail "финский адрес без домена должен уйти в Shadowsocks"; }
ok "«не Россия» в туннель, Steam напрямую: steam.test пошёл напрямую, тот же адрес без домена — через Shadowsocks"

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
' || { echo "$STATS"; fail "статистика не посчитала трафик туннелей"; }
HOSTS=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/stats/hosts?tag=vless")
echo "$HOSTS" | grep -q '203.0.113.81\|hidden.test' || { echo "$HOSTS"; fail "нет хоста в таблице трафика VLESS"; }
ok "API статистики: байты по ss, vless и direct посчитаны, хост VLESS в таблице"

# --- Verify route explanations, audit events and on-disk journal export
ROUTES=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/routes?limit=200")
echo "$ROUTES" | python3 -c '
import json, sys
e = json.load(sys.stdin)["entries"]
def find(host, out):
    for x in e:
        if x["host"] == host and x["outbound"] == out:
            return x
    raise SystemExit("нет записи %s → %s: %s" % (host, out, [(x["host"], x["outbound"], x["rule"]) for x in e]))
assert find("steam.test", "direct")["rule"] == "сеть «LAN»: группа «Steam напрямую»", find("steam.test", "direct")
assert find("hidden.test", "vless")["rule"] == "сеть «LAN»: группа «Через VLESS»", find("hidden.test", "vless")
ss = find("198.51.100.4", "ss")
assert ss["rule"] == "сеть «LAN»: группа «Всё кроме России» (все, кроме RU)", ss
assert ss["client"].startswith("192.168.77.10:"), ss
' || { echo "$ROUTES" | head -c 3000; fail "журнал маршрутизации"; }
AUDIT=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/audit")
echo "$AUDIT" | grep -q 'Запуск keenProxy' || { echo "$AUDIT"; fail "нет записи о запуске в аудите"; }
[ -s /tmp/journal/routes.jsonl ] && [ -s /tmp/journal/audit.jsonl ] || { ls -la /tmp/journal; fail "журналы не пишутся на диск"; }
curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/routes/export?since=1h" | grep -q 'steam.test.*Steam напрямую' || fail "выгрузка журнала"
ok "журнал: steam.test — «Steam напрямую», финский адрес — «всё, кроме России», аудит запуска, файлы и выгрузка"

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
iptables -t nat -C PREROUTING -j KEENPROXY_TCP 2>/dev/null || fail "сторож не вернул снятый переход"
ip netns exec client curl -s -m 5 -o /dev/null http://198.51.100.3/ || fail "HTTP после восстановления"
ok "сторож вернул снятый переход, HTTP снова работает"
curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/audit?actor=watchdog" | grep -q 'Правила перехвата восстановлены' || fail "нет записи сторожа в аудите"
# A direct connection to a closed port must produce an engine error record.
ip netns exec client curl -s -m 3 -o /dev/null http://198.51.100.3:81/ || true
SYS=""
for i in $(seq 1 25); do
	SYS=$(curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086/api/journal/system?source=sing-box")
	echo "$SYS" | grep -q '"source":"sing-box"' && break
	sleep 0.2
done
echo "$SYS" | grep -q '"source":"sing-box"' || { echo "$SYS"; tail -20 /tmp/keenproxy.log; fail "журнал sing-box не собирается"; }
ok "аудит записал восстановление правил сторожем, ошибки sing-box попадают в системный журнал"

# --- Fail over from the offline primary, then recover after the up hysteresis
api() { curl -s -m 5 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:8086$1"; }
waitfor() { # путь API, шаблон, секунд
	for i in $(seq 1 $(($3 * 5))); do
		api "$1" | grep -q "$2" && return 0
		sleep 0.2
	done
	api "$1"; tail -20 /tmp/keenproxy.log; return 1
}
# Schedule a request and verify its host, actual outbound and route explanation.
route_is() {
	ip netns exec client curl -s -m 5 -o /dev/null -H 'Host: backup.test' http://203.0.113.80/ || fail "backup.test недоступен"
	api "/api/journal/routes?limit=20" | python3 -c '
import json, sys
out, note = sys.argv[1], sys.argv[2]
e = [x for x in json.load(sys.stdin)["entries"] if x["host"] == "backup.test"][0]
assert e["outbound"] == out and note in e["rule"], e
' "$1" "$2" || fail "backup.test должен идти через $1"
}
waitfor "/api/journal/audit?target=main" 'Переключение на резервное' 10 || fail "не переключилось на резервное"
waitfor "/api/status" '"main":{"active":"vless"' 2 || fail "статус не показывает резервное"
route_is vless "резервное: «Основное» не отвечает"
ok "основное не отвечает — трафик через резервное (vless), аудит и журнал это показывают"
ip netns exec wan /work/sbserver /tmp/server-main.json >/tmp/sbserver-main.log 2>&1 &
waitfor "/api/journal/audit?target=main" 'Возврат на основное' 15 || fail "не вернулось на основное"
route_is main "группа «С резервным»"
grep -q 'inbound/shadowsocks.*203.0.113.80' /tmp/sbserver-main.log || fail "сервер основного не видит соединения"
ok "основное ожило — через up_after трафик вернулся на него"

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
	fail "после остановки остались правила"
fi
ok "после SIGTERM правил KEENPROXY не осталось"
echo "PASS"
