#!/opt/bin/sh
# Recovery for the explicitly approved redirect-mode trial on this router only.
set -eu
PATH=/opt/sbin:/opt/bin:/opt/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
stage=/opt/keenproxy-test-20261106
umask 077
[ -f "$stage/switch-started" ] || exit 0
mkdir "$stage/rollback.lock" 2>/dev/null || exit 1
trap 'rmdir "$stage/rollback.lock" 2>/dev/null || true' EXIT
exec >>"$stage/rollback.log" 2>&1
date -u

if [ -f "$stage/keenproxy.pid" ]; then
    read pid < "$stage/keenproxy.pid"
    case "$pid" in ''|*[!0-9]*) echo 'Invalid test PID'; exit 1 ;; esac
    exe=$(readlink "/proc/$pid/exe" 2>/dev/null || true)
    if [ "$exe" = "$stage/keenproxy" ]; then
        kill -TERM "$pid" 2>/dev/null || true
        n=0
        while [ "$n" -lt 15 ] && kill -0 "$pid" 2>/dev/null; do
            sleep 1
            n=$((n+1))
        done
        exe=$(readlink "/proc/$pid/exe" 2>/dev/null || true)
        if [ "$exe" = "$stage/keenproxy" ]; then
            kill -KILL "$pid" 2>/dev/null || true
            sleep 1
        fi
    elif [ -n "$exe" ]; then
        echo 'PID now belongs to another executable; refusing to stop it'
        exit 1
    fi
fi

# Never flush built-in chains or restore an old snapshot over live NDM rules.
for row in 'nat PREROUTING KEENPROXY_DNS' 'nat PREROUTING KEENPROXY_TCP' \
           'filter INPUT KEENPROXY_DNS_GUARD' 'filter FORWARD KEENPROXY_DNS_GUARD' \
           'filter FORWARD KEENPROXY_QUIC'; do
    set -- $row
    while /opt/sbin/iptables -w -t "$1" -D "$2" -j "$3" 2>/dev/null; do :; done
done
for row in 'nat KEENPROXY_DNS' 'nat KEENPROXY_TCP' \
           'filter KEENPROXY_DNS_GUARD' 'filter KEENPROXY_QUIC'; do
    set -- $row
    if /opt/sbin/iptables -w -t "$1" -S "$2" >/dev/null 2>&1; then
        /opt/sbin/iptables -w -t "$1" -F "$2"
        /opt/sbin/iptables -w -t "$1" -X "$2"
    fi
done
if [ -f "$stage/99-kvaspro.sh.disabled" ]; then
    if [ -e /opt/etc/ndm/netfilter.d/99-kvaspro.sh ]; then
        echo 'NDM hook destination unexpectedly occupied; original preserved'
        exit 1
    fi
    mv "$stage/99-kvaspro.sh.disabled" /opt/etc/ndm/netfilter.d/99-kvaspro.sh
fi
/opt/etc/init.d/S99sing-box start
/opt/etc/init.d/S99kvaspro start
/opt/bin/kvaspro --config /opt/etc/kvaspro/kvaspro.toml --reapply-lan
/opt/etc/init.d/S99sing-box status
/opt/etc/init.d/S99kvaspro status
mv "$stage/switch-started" "$stage/rolled-back"
date -u
echo 'Previous services restored'
