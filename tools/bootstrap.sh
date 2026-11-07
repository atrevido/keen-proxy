#!/bin/sh
# Manage a KeenProxy release on an Entware router.
(
set -eu
umask 077

fail() { printf 'KeenProxy: %s\n' "$*" >&2; exit 1; }
usage() {
    printf '%s\n' 'Usage: sh -s -- [--version vX.Y.Z] [--lite] [--without-trusttunnel]' \
        '       sh -s -- --uninstall'
}

prefix=${KEENPROXY_PREFIX:-/opt}
export KEENPROXY_PREFIX="$prefix"
PATH="$prefix/bin:$prefix/sbin:$PATH"
export PATH
main=https://raw.githubusercontent.com/ordo-systems/keen-proxy/main
releases=https://github.com/ordo-systems/keen-proxy/releases/download
version=
variant=
trusttunnel=1
uninstall=0
while [ "$#" -gt 0 ]; do
    case "$1" in
        --version) [ "$#" -ge 2 ] && [ -n "$2" ] || fail 'missing --version value'; version=$2; shift ;;
        --lite) variant=lite- ;;
        --without-trusttunnel) trusttunnel=0 ;;
        --uninstall) uninstall=1 ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; fail "unknown option: $1" ;;
    esac
    shift
done
if [ "$uninstall" = 1 ] && { [ -n "$version$variant" ] || [ "$trusttunnel" = 0 ]; }; then
    fail '--uninstall cannot be combined with installation options'
fi

case "$prefix" in /*) ;; *) fail 'Entware prefix must be absolute' ;; esac
[ -x "$prefix/etc/init.d/rc.unslung" ] || fail 'install Entware before running this script'
if [ "$prefix" = /opt ] && [ "$(id -u)" != 0 ]; then
    fail 'run this script as root in the Entware shell'
fi
command -v pidof >/dev/null 2>&1 || fail 'missing dependency: pidof'

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

lock_service() {
    state="$prefix/var/run/keenproxy"
    if [ "$prefix" = /opt ]; then state=/var/run/keenproxy; fi
    mkdir -p "$state"
    mkdir "$state/action.lock" 2>/dev/null || fail 'service action already in progress'
    service_lock="$state/action.lock"
    printf '%s\n' "$work" > "$service_lock/owner"
}

# Use the transaction's controller copy, even after replacing or removing the
# installed script. Installation uses the verified release controller.
service_action() {
    KEENPROXY_SERVICE_LOCK_TOKEN="$work" sh "$work/S99keenproxy" "$1"
}

managed_service() {
    [ -f "$service" ] && [ ! -L "$service" ] && grep -Fxq '# KeenProxy Entware service' "$service"
}

backup_file() {
    if [ -e "$prefix/$1" ] || [ -L "$prefix/$1" ]; then
        [ -f "$prefix/$1" ] && [ ! -L "$prefix/$1" ] || fail "unsupported destination: $1"
        mkdir -p "$work/previous/${1%/*}"
        cp -p "$prefix/$1" "$work/previous/$1"
    fi
}

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
    keep_work=0
    if [ "$replacing" = 1 ]; then
        echo 'KeenProxy: restoring the previous installation' >&2
        if { [ "$uninstall" = 0 ] && [ -f "$service" ] && ! service_action stop; } || ! restore_payload; then
            echo "KeenProxy: rollback failed; recovery files retained at $work" >&2
            keep_work=1
            result=1
        fi
    fi
    if [ "$restart_previous" = 1 ] && [ "$keep_work" = 0 ]; then
        service_action start || echo 'KeenProxy: previous service did not restart; check its status' >&2
    fi
    if [ -n "$service_lock" ]; then
        rm -f "$service_lock/owner" || :
        rmdir "$service_lock" || :
    fi
    if [ -n "$work" ] && [ "$keep_work" = 0 ]; then rm -rf "$work"; fi
    rmdir "$lock" || :
    exit "$result"
}

uninstall_release() {
    lock_service
    if [ ! -e "$service" ] && [ ! -L "$service" ] &&
        [ ! -e "$prefix/bin/keenproxy" ] && [ ! -L "$prefix/bin/keenproxy" ]; then
        echo 'KeenProxy: no managed installation found'
        return
    fi
    managed_service || fail 'unmanaged installation; no files removed'
    grep -Fq 'KEENPROXY_SERVICE_LOCK_TOKEN' "$service" || fail 'init script does not support installer locking; no files removed'
    for path in $payload; do backup_file "$path"; done
    cp -p "$work/previous/etc/init.d/S99keenproxy" "$work/S99keenproxy"
    if service_action check; then
        restart_previous=1
        service_action stop || fail 'service did not stop cleanly; no files removed'
    elif pidof keenproxy >/dev/null 2>&1; then
        fail 'unmanaged process is running; no files removed'
    fi
    replacing=1
    for path in $payload; do rm -f "$prefix/$path" || fail "could not remove $path"; done
    replacing=0
    restart_previous=0
    printf 'KeenProxy uninstalled. Settings retained in %s/etc/keenproxy\n' "$prefix"
}

service="$prefix/etc/init.d/S99keenproxy"
payload='bin/keenproxy etc/init.d/S99keenproxy'
mkdir -p "$prefix/tmp" "$prefix/var/lock"
lock="$prefix/var/lock/keenproxy-install.lock"
mkdir "$lock" 2>/dev/null || fail 'another installer is active or an interrupted run left a lock'
work=
service_lock=
replacing=0
was_running=0
restart_previous=0
trap finish EXIT
trap 'exit 1' HUP INT TERM
work=$(mktemp -d "$prefix/tmp/keenproxy-release.XXXXXX")
if [ "$uninstall" = 1 ]; then
    uninstall_release
    exit 0
fi

for tool in curl sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || fail "missing dependency: $tool"
done
arch=$(architecture)
asset="keenproxy-${variant}linux-$arch"
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
"$work/$asset" -dry-run -config "$config_source" >/dev/null 2>&1 || fail 'candidate binary or configuration failed validation'

if [ -e "$service" ] || [ -L "$service" ]; then
    managed_service || fail 'existing init script is not managed by this installer'
fi
ensure_firewall_tools

set -- --prefix "$work/payload" --binary "$work/$asset"
if [ "$trusttunnel" = 0 ]; then set -- "$@" --without-trusttunnel; fi
sh "$work/install.sh" "$@" || fail 'could not stage release files'
mkdir -p "$work/payload/etc/init.d"
chmod 755 "$work/S99keenproxy"
cp -p "$work/S99keenproxy" "$work/payload/etc/init.d/S99keenproxy"
if [ "$trusttunnel" = 1 ]; then
    payload="$payload bin/trusttunnel_client share/keenproxy/trusttunnel/LICENSE share/keenproxy/trusttunnel/version"
fi

lock_service
if [ -f "$service" ] && service_action check; then was_running=1; fi
if [ "$was_running" = 0 ] && pidof keenproxy >/dev/null 2>&1; then
    fail 'stop the existing unmanaged KeenProxy process before installing'
fi
changed=0
for path in $payload; do
    backup_file "$path"
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
