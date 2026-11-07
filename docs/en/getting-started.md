# Getting started

## Prepare the router

### Hardware and firmware

- **CPU.** KeenProxy publishes Linux ARM64 (`aarch64`) and little-endian MIPS (`mipsle` or `mipsel`, soft float) builds. Look up the exact model number in the router manual. For MIPS, confirm the byte order with the Entware package architecture or the model guide.
- **Firmware.** Use a Keenetic or Netcraze model with Open Package support (OPKG), an Entware shell, and compatible `iptables` tools. TCP interception uses REDIRECT. UDP interception in `tproxy` mode also requires kernel TPROXY support and policy routing.
- **Storage.** Compare the release binary size with the free space on `/opt`. Leave room for Entware, the configuration, rule sets and filter data, and a rollback copy. Use an external drive if the internal partition is tight. The lite binary is smaller and supports fewer protocols.
- **RAM.** The project has no validated minimum across models. Check available memory with your usual router services running. Leave headroom for the engine, connections, and enabled lists, and monitor memory during the first test with your own configuration.

**Recommended firmware component.** Enable **Kernel modules for Netfilter** alongside OPKG if your model offers it. It supplies kernel support for packet filtering used by `iptables`; the Entware commands are a separate prerequisite. Check the component list for your exact model. [KeeneticOS component reference](https://support.keenetic.com/hopper/kn-3810/en/16327-os-component-description.html)

### Choose where to install Entware

| Method | How it works |
| --- | --- |
| **USB drive** | Back up the drive, format it as ext4, enable OPKG and the required filesystem support, then place the model-specific installer archive in the drive's `install` folder. Select that drive in OPKG Package Manager. This is the practical choice when you need room for packages, lists, and backups. [Keenetic USB guide](https://support.keenetic.com/hopper-se/kn-3812/en/20980-installing-the-entware-repository-on-a-usb-drive.html) |
| **Internal storage** | Supported on selected models and firmware versions. Select Internal storage in OPKG Package Manager where the option is available. The vendor guide also describes a CLI download and a manual archive method. Check your model's manual and available space before choosing this option. [Keenetic internal storage guide](https://support.keenetic.com/titan/kn-1811/en/18482-installing-opkg-entware-in-the-router-s-internal-memory.html) |

The linked manuals use specific models as examples. Use the components and installer archive listed for your own model.

### Verify the environment

After Entware setup, connect to the Entware shell from the LAN and change its initial password. Back up the router settings and keep local access for recovery. Run these read-only checks before copying KeenProxy:

```sh
uname -m
opkg print-architecture
df -h /opt
free
command -v iptables
command -v iptables-restore
iptables -t nat -S
```

Confirm the architecture, free disk space, available memory, and firewall tools. Start with an example configuration and replace its placeholder connection details privately.

The last command lists IPv4 NAT rules without editing them. For `tproxy` mode, also check that `iptables -t mangle -S` succeeds. Table access is a preliminary check. Verify REDIRECT or TPROXY with traffic from a test client before relying on interception.

## Install or update from GitHub

Install `curl` and the CA certificates in the Entware root shell if needed.

```sh
opkg update && opkg install curl ca-bundle ca-certificates
```

Stop competing traffic or DNS interception services before installation. KeenProxy begins intercepting LAN DNS when the daemon starts, including with the starter configuration. Run the bootstrap from `main`:

```sh
curl -fsSL https://raw.githubusercontent.com/ordo-systems/keen-proxy/main/tools/bootstrap.sh | /opt/bin/sh
```

The installer reads `version.txt` from `main`, detects the CPU, and verifies the release files against `SHA256SUMS`. It installs missing Entware `iptables` tools, creates a starter configuration if needed, and registers and starts `S99keenproxy` through the Entware init mechanism. TrustTunnel is installed by default as an optional client launched by KeenProxy when configured. With the starter configuration, open the router's LAN address on port `8086`. Read the token on the router from `/opt/etc/keenproxy/token`.

Run the same command to update. Your configuration and token are preserved. The installer stages and validates the candidate before stopping the managed service. If the new service fails its startup check, it attempts to restore the previous files and restart the previous service. A manually started instance must be stopped before the installer can take over.

To choose a release, replace `vX.Y.Z` with a published tag. The optional `--lite` flag selects the smaller build. `--without-trusttunnel` skips installation of the external client.

```sh
curl -fsSL https://raw.githubusercontent.com/ordo-systems/keen-proxy/main/tools/bootstrap.sh | /opt/bin/sh -s -- --version vX.Y.Z
```

The tag must have installer assets and checksums attached. If publication is still in progress, wait for the assets and rerun the command. Service commands are `status`, `start`, `stop`, and `restart` on `/opt/etc/init.d/S99keenproxy`. See the root README for manual installation. [Installation reference](https://github.com/ordo-systems/keen-proxy#configuration-and-router-installation)

## First configuration

Follow the [user guide](user-guide.md): inspect a connection, add a domain group, choose a network/client policy, Apply, then verify using Diagnostics and Journal. Start with a small set of domains and one test device.

## Updates and recovery

Back up the current configuration and keep the old binary before updating. Read release compatibility and migration notes. Settings can preview a previous configuration into the shared draft; applying it still requires **Apply**. Do not assume a successful HTTP probe establishes that all HTTPS traffic works.

## Uninstall

Run from the Entware root shell:

```sh
curl -fsSL https://raw.githubusercontent.com/ordo-systems/keen-proxy/main/tools/bootstrap.sh | /opt/bin/sh -s -- --uninstall
```

The command stops the managed service and removes `/opt/bin/keenproxy` and `/opt/etc/init.d/S99keenproxy`. It preserves `/opt/etc/keenproxy/` (including configuration and token), `/opt/var/lib/keenproxy/` (logs and derived state), and the optional TrustTunnel client. Run the regular installer again to reuse your settings.
