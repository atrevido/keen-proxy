#!/bin/sh
# Entware installer. Run with /opt/bin/sh on Keenetic (firmware /bin/sh differs).
set -eu

fail() { echo "keenProxy: $*" >&2; exit 1; }
usage() {
    echo 'Usage: install.sh [--prefix /opt] --binary PATH [--without-trusttunnel]'
    echo '       install.sh [--prefix /opt] --trusttunnel-only'
    echo 'Installs files only; preserves configuration and does not start/restart services.'
}

prefix=/opt
binary=
tt_only=0
with_tt=1
while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix|--binary)
            [ "$#" -ge 2 ] || fail "missing value for $1"
            case "$1" in --prefix) prefix=$2 ;; --binary) binary=$2 ;; esac
            shift 2 ;;
        --trusttunnel-only) tt_only=1; shift ;;
        --without-trusttunnel) with_tt=0; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; fail "unknown argument: $1" ;;
    esac
done
case "$prefix" in /*) ;; *) fail 'prefix must be an absolute path' ;; esac
if [ "$tt_only" = 1 ]; then
    [ -z "$binary" ] && [ "$with_tt" = 1 ] || fail '--trusttunnel-only cannot be combined with --binary/--without-trusttunnel'
else
    [ -n "$binary" ] && [ -f "$binary" ] && [ -x "$binary" ] || fail '--binary must name an executable keenProxy build for this router'
fi

# Work on the destination filesystem so replacement by rename is atomic.
mkdir -p "$prefix/bin" "$prefix/tmp"
work=$(mktemp -d "$prefix/tmp/keenproxy-install.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM

if [ "$with_tt" = 1 ]; then
    version=1.1.11
    case "$(uname -m)" in
        aarch64|arm64)
            arch=aarch64; digest=880e9c8afd5cce7ff38c53978f0ea530ff95e679bcfef69c5127b26b3692d110 ;;
        armv7l|armv8l)
            arch=armv7; digest=90c032f1b6de3bc797f8d144582805b565190e56fd5636a5eed089d7458ac1bc ;;
        x86_64|amd64)
            arch=x86_64; digest=00e0546a6b35840dbb92b741dff3da6732a74bf2d6c581e044bb4567c0d96825 ;;
        mips)
            # ELF EI_DATA byte: BusyBox hexdump is available on Entware routers.
            endian=$(head -c 6 /proc/self/exe | hexdump -v -e '1/1 "%02x"')
            case "$endian" in
                *01) arch=mipsel; digest=fa4ca450e01f0ebacc6ee3f5865b8f31912c6edbe164929c33732db2968951dc ;;
                *02) arch=mips; digest=376b834e87f8987c92b2a96765379fa74cf27cb591c3a4bb4b7f0f483168cd92 ;;
                *) fail 'cannot determine MIPS byte order' ;;
            esac ;;
        mipsel|mipsle)
            arch=mipsel; digest=fa4ca450e01f0ebacc6ee3f5865b8f31912c6edbe164929c33732db2968951dc ;;
        *) fail 'unsupported TrustTunnel CPU architecture' ;;
    esac
    for tool in curl sha256sum tar; do command -v "$tool" >/dev/null 2>&1 || fail "install Entware dependency: $tool"; done
    package="trusttunnel_client-v${version}-linux-${arch}"
    url="https://github.com/TrustTunnel/TrustTunnelClient/releases/download/v${version}/${package}.tar.gz"
    echo "Downloading TrustTunnel ${version} (${arch})"
    # Go's CA discovery and curl's defaults differ on some Entware installations.
    if [ -f "$prefix/etc/ssl/certs/ca-certificates.crt" ]; then
        curl --cacert "$prefix/etc/ssl/certs/ca-certificates.crt" -fsSL --connect-timeout 20 --max-time 180 --retry 2 "$url" -o "$work/client.tar.gz"
    else
        curl -fsSL --connect-timeout 20 --max-time 180 --retry 2 "$url" -o "$work/client.tar.gz"
    fi
    actual=$(sha256sum "$work/client.tar.gz")
    actual=${actual%% *}
    [ "$actual" = "$digest" ] || fail 'TrustTunnel SHA256 mismatch; installed files unchanged'
    # Extract only these regular files from the verified, pinned release.
    tar -xzf "$work/client.tar.gz" -C "$work" "$package/trusttunnel_client" "$package/LICENSE"
    client="$work/$package/trusttunnel_client"
    [ -f "$client" ] && [ ! -L "$client" ] || fail 'client missing from archive'
    chmod 755 "$client"
    # --help exits 1 in the upstream CLI; --version exits successfully.
    "$client" --version || fail 'TrustTunnel cannot run on this router; installed files unchanged'
fi

# Stage all payloads before replacing existing files.
if [ "$tt_only" = 0 ]; then
    cp "$binary" "$work/keenproxy"
    chmod 755 "$work/keenproxy"
fi
if [ "$with_tt" = 1 ]; then
    mkdir -p "$prefix/share/keenproxy/trusttunnel"
    cp "$work/$package/LICENSE" "$prefix/share/keenproxy/trusttunnel/LICENSE"
    mv -f "$client" "$prefix/bin/trusttunnel_client"
    printf '%s\n' "$version" > "$prefix/share/keenproxy/trusttunnel/version"
fi
if [ "$tt_only" = 0 ]; then
    mv -f "$work/keenproxy" "$prefix/bin/keenproxy"
fi
echo "Installed into $prefix/bin. Configuration preserved. Restart keenProxy to load the client."
