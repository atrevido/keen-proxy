# Getting started

## Before installation

You need a compatible router with Entware, SSH access, enough storage and memory, and a recovery path. Confirm your CPU architecture and the interception capabilities required by your configuration. ARM64 and little-endian MIPS builds are separate files. Do not install an arbitrary binary based only on the router's marketing name.

Prepare a backup and retain SSH/local access. Start with a synthetic example and replace its placeholder connection details privately. The demo's addresses and keys are examples, not working VPN credentials.

## Installation outline

1. Obtain the matching KeenProxy binary and installer from a reviewed release. Compare the downloaded files with its `SHA256SUMS`.
2. Copy the files to the router and review the installer options.
3. Install the binary with your configuration. The installer preserves an existing configuration; it does not configure autostart or start/restart services.
4. Start the daemon using your reviewed local service setup and open the management address configured in `api.listen`.
5. Authenticate using the configured token, unless your address belongs to an explicitly trusted network.

The root [README on GitHub](https://github.com/atrevido/keen-proxy#configuration-and-router-installation) is the executable installation reference. This initial guide deliberately does not prescribe firmware-specific service commands.

## First configuration

Follow the [user guide](user-guide.md): inspect a connection, add a domain group, choose a network/client policy, Apply, then verify using Diagnostics and Journal. Start with a small set of domains and one test device.

## Updates and recovery

Back up the current configuration and keep the old binary before updating. Read release compatibility and migration notes. Settings can preview a previous configuration into the shared draft; applying it still requires **Apply**. Do not assume a successful HTTP probe establishes that all HTTPS traffic works.
