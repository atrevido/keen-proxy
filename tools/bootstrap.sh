#!/bin/sh
# Download and install a KeenProxy release on an Entware router.
(
set -eu
umask 077

fail() { printf 'KeenProxy: %s\n' "$*" >&2; exit 1; }
usage() { echo 'Usage: sh -s -- [--version vX.Y.Z] [--lite] [--without-trusttunnel]'; }

prefix=${KEENPROXY_PREFIX:-/opt}
export KEENPROXY_PREFIX="$prefix"
PATH="$prefix/bin:$prefix/sbin:$PATH"
export PATH
main=https://raw.githubusercontent.com/atrevido/keen-proxy/main
releases=https://github.com/atrevido/keen-proxy/releases/download
version=
variant=
trusttunnel=1
while [ "$#" -gt 0 ]; do
    case "$1" in
        --version) [ "$#" -ge 2 ] && [ -n "$2" ] || fail 'missing --version value'; version=$2; shift ;;
        --lite) variant=lite- ;;
        --without-trusttunnel) trusttunnel=0 ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; fail "unknown option: $1" ;;
    esac
    shift
done

case "$prefix" in /*) ;; *) fail 'Entware prefix must be absolute' ;; esac
[ -x "$prefix/etc/init.d/rc.unslung" ] || fail 'install Entware before running this script'
if [ "$prefix" = /opt ] && [ "$(id -u)" != 0 ]; then
    fail 'run this script as root in the Entware shell'
fi
for tool in curl sha256sum pidof; do
    command -v "$tool" >/dev/null 2>&1 || fail "missing dependency: $tool"
done

architecture() {
    case "$(uname -m)" in
        aarch64|arm64) echo arm64 ;;
        mipsel|mipsle) echo mipsle ;;
        mips)
            # The kernel may report mips for either byte order. Inspect ELF EI_DATA.
            byte_order=$(head -c 6 /proc/self/exe | hexdump -v -e '1/1 "%02x"')
            case "$byte_order" in
                7f454c46??01) echo mipsle ;;
                *) fail 'KeenProxy requires little-endian MIPS' ;;
            esac ;;
        *) fail 'KeenProxy releases support ARM64 and little-endian MIPS' ;;
    esac
}

fetch() {
    curl --proto '=https' --proto-redir '=https' -fsSL --connect-timeout 20 \
        --max-time 240 --retry 2 "$1" -o "$2" || fail "download failed: $1"
}

verify() {
    expected=$(awk -v name="$1" '$2 == name {print $1}' "$work/SHA256SUMS")
    case "$expected" in *[!0-9a-fA-F]*) fail "invalid SHA256 entry for $1" ;; esac
    [ "${#expected}" -eq 64 ] || fail "missing or invalid SHA256 entry for $1"
    actual=$(sha256sum "$work/$1")
    [ "${actual%% *}" = "$expected" ] || fail "SHA256 mismatch for $1"
}

ensure_firewall_tools() {
    if [ ! -x "$prefix/sbin/iptables" ] || [ ! -x "$prefix/sbin/iptables-restore" ]; then
        [ -x "$prefix/bin/opkg" ] || fail 'opkg is required to install iptables'
        "$prefix/bin/opkg" update && "$prefix/bin/opkg" install iptables || fail 'cannot install Entware iptables'
    fi
    [ -x "$prefix/sbin/iptables" ] && [ -x "$prefix/sbin/iptables-restore" ] || fail 'Entware iptables tools are missing'
    "$prefix/sbin/iptables" -t nat -S >/dev/null || fail 'IPv4 NAT unavailable; check the firmware Netfilter component'
}

service_action() { sh "$prefix/etc/init.d/S99keenproxy" "$1"; }

install_payload() {
    for path in $payload; do
        mkdir -p "$prefix/${path%/*}" || return 1
        mv -f "$work/payload/$path" "$prefix/$path" || return 1
    done
}

restore_payload() {
    for path in $payload; do
        if [ -f "$work/previous/$path" ]; then
            mv -f "$work/previous/$path" "$prefix/$path" || return 1
        else
            rm -f "$prefix/$path" || return 1
        fi
    done
}

finish() {
    result=$?
    trap - EXIT
    if [ "$replacing" = 1 ]; then
        echo 'KeenProxy: restoring the previous installation' >&2
        if { [ -f "$prefix/etc/init.d/S99keenproxy" ] && ! service_action stop; } || ! restore_payload; then
            echo "KeenProxy: rollback failed; recovery files retained at $work" >&2
            rmdir "$lock" || :
            exit 1
        fi
    fi
    if [ "$restart_previous" = 1 ]; then
        service_action start || echo 'KeenProxy: previous service did not restart; check its status' >&2
    fi
    [ -z "$work" ] || rm -rf "$work"
    rmdir "$lock" || :
    exit "$result"
}

arch=$(architecture)
asset="keenproxy-${variant}linux-$arch"
mkdir -p "$prefix/tmp" "$prefix/var/lock"
lock="$prefix/var/lock/keenproxy-install.lock"
mkdir "$lock" 2>/dev/null || fail 'another installer is active or an interrupted run left a lock'
work=
replacing=0
was_running=0
restart_previous=0
trap finish EXIT
trap 'exit 1' HUP INT TERM
work=$(mktemp -d "$prefix/tmp/keenproxy-release.XXXXXX")
if [ -f "$prefix/etc/ssl/certs/ca-certificates.crt" ]; then
    export CURL_CA_BUNDLE="$prefix/etc/ssl/certs/ca-certificates.crt"
fi
if [ -z "$version" ]; then
    fetch "$main/version.txt" "$work/version.txt"
    version=$(cat "$work/version.txt")
fi
version=${version#v}
case "$version" in ''|*[!0-9A-Za-z.-]*) fail 'invalid release version' ;; esac
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' || fail 'invalid release version'
tag="v$version"
base="$releases/$tag"
fetch "$base/SHA256SUMS" "$work/SHA256SUMS"
for name in "$asset" install.sh S99keenproxy; do
    fetch "$base/$name" "$work/$name"
    verify "$name"
done

config="$prefix/etc/keenproxy/config.yaml"
config_source="$config"
if [ ! -f "$config" ]; then
    printf 'schema_version: 1\n' > "$work/config.yaml"
    config_source="$work/config.yaml"
fi
chmod 755 "$work/$asset"
# Dry-run output can contain connection credentials. Keep it off the console.
"$work/$asset" -dry-run -config "$config_source" >/dev/null || fail 'candidate binary or configuration failed validation'

service="$prefix/etc/init.d/S99keenproxy"
if [ -e "$service" ] || [ -L "$service" ]; then
    [ -f "$service" ] && [ ! -L "$service" ] && grep -Fxq '# KeenProxy Entware service' "$service" || fail 'existing init script is not managed by this installer'
    if service_action check; then was_running=1; fi
fi
if [ "$was_running" = 0 ] && pidof keenproxy >/dev/null 2>&1; then
    fail 'stop the existing unmanaged KeenProxy process before installing'
fi
ensure_firewall_tools

set -- --prefix "$work/payload" --binary "$work/$asset"
if [ "$trusttunnel" = 0 ]; then set -- "$@" --without-trusttunnel; fi
sh "$work/install.sh" "$@" || fail 'could not stage release files'
mkdir -p "$work/payload/etc/init.d"
chmod 755 "$work/S99keenproxy"
mv "$work/S99keenproxy" "$work/payload/etc/init.d/S99keenproxy"
payload='bin/keenproxy etc/init.d/S99keenproxy'
if [ "$trusttunnel" = 1 ]; then
    payload="$payload bin/trusttunnel_client share/keenproxy/trusttunnel/LICENSE share/keenproxy/trusttunnel/version"
fi

changed=0
for path in $payload; do
    if [ -e "$prefix/$path" ] || [ -L "$prefix/$path" ]; then
        [ -f "$prefix/$path" ] && [ ! -L "$prefix/$path" ] || fail "unsupported destination: $path"
        mkdir -p "$work/previous/${path%/*}"
        cp -p "$prefix/$path" "$work/previous/$path"
    fi
    if ! cmp -s "$work/payload/$path" "$prefix/$path"; then changed=1; fi
    case "$path" in
        bin/*|etc/init.d/*) if [ ! -x "$prefix/$path" ]; then changed=1; fi ;;
    esac
done
if [ "$config_source" != "$config" ]; then
    mkdir -p "${config%/*}"
    # Avoid overwriting a configuration created while the release was downloading.
    (set -C; cat "$config_source" > "$config") || fail 'could not create initial configuration'
fi
if [ "$changed" = 1 ]; then
    if [ "$was_running" = 1 ]; then
        restart_previous=1
        service_action stop || fail 'previous service did not stop cleanly'
    fi
    replacing=1
    install_payload || fail 'could not install staged files'
    service_action start || fail 'new service failed to start'
    restart_previous=0
    replacing=0
elif [ "$was_running" = 0 ]; then
    service_action start || fail 'service failed to start'
fi
printf 'KeenProxy %s (%s) installed. Configuration: %s\n' "$tag" "$asset" "$config"
)
