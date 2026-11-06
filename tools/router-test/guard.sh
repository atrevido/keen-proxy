#!/opt/bin/sh
# Launched detached; restore the old services unless the operator verifies health.
set -eu
stage=/opt/keenproxy-test-20261106
if [ "${1:-}" = probe ]; then
    sleep 3
    : > "$stage/guard-probe-ok"
    exit 0
fi
sleep 900
[ -f "$stage/accepted" ] && exit 0
exec /opt/bin/sh "$stage/rollback.sh"
