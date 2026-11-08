# AmneziaWG 3.1

The first release targets ARM64 and proxies application TCP. It accepts a native AmneziaWG 3.1 configuration file, including exports from Amnezia Premium. Check the export against your provider before replacing a working route. The AWG tunnel itself uses UDP. Application UDP and changes to DNS policy are outside this feature.

Build the client with `make build-awg-arm64`. The output is `dist/keenproxy-awg-linux-arm64`.

## Keenetic and Netcraze with Entware

Install the optional ARM64 binary at `/opt/bin/keenproxy-awg` with execute permission. In KeenProxy, choose **New connection → AmneziaWG** and select the native `.conf`. Use **Test HTTPS through AmneziaWG** before adding it to the shared draft. Assign the profile to a test routing rule and apply the draft.

The first activation and subsequent routing changes are saved for the next planned service start. The existing WireGuard and VLESS sessions continue while the draft is staged. Replacing only the profile file on the same connection and SOCKS port restarts only the AWG process. An edited file with the same active peer identity cannot be tested concurrently. The existing identity may be tested through its running SOCKS endpoint.

The pending draft is kept alongside the active YAML as `config.yaml.pending-awg` with restricted permissions. Back up the active file before a planned restart. If you need to discard the pending change, load the active YAML backup from the panel and apply it again. The daemon attempts to restore the active configuration if the staged configuration fails to start.

## MikroTik RouterOS

This route uses RouterOS Container and Socksify. Create a dedicated transit interface and assign an isolated IPv4 subnet. Allow the router to reach the container's SOCKS port from its transit address. Keep this port off the WAN and ordinary LAN. Confirm that the selected RouterOS device has the matching ARM64 Container package and that the container device mode is enabled.

Build the archive from the repository root:

```sh
make build-awg-arm64
docker build --platform linux/arm64 -t keenproxy-awg:local -f tools/routeros/awg.Dockerfile dist
docker save -o keenproxy-awg-arm64.tar keenproxy-awg:local
```

Import the archive into RouterOS. Mount a directory containing `awg.conf` at `/config` with permissions that allow UID/GID 65534 to read it but not write it. Set the container DNS to a resolver reachable from its transit interface, especially if the AWG endpoint is a hostname. Run the container unprivileged with the following command arguments, replacing both example addresses with those of the dedicated transit link:

```text
-config /config/awg.conf -socks-listen 192.0.2.2:1080 -allow-from 192.0.2.1
```

Configure a Socksify service pointing to the container at TCP port 1080. Add a destination NAT rule with `action=socksify`, `protocol=tcp`, `dst-address-type=!local`, and an explicit pilot client and destination match. Review filter input permission to the Socksify port for that pilot and ensure the matching TCP is not FastTracked. Test HTTPS from both the pilot and an unrelated client. Disable or remove only the AWG-specific rule and service to restore the prior routing policy.

Remove the Premium subscription token from the file before uploading it to RouterOS. Example for a prepared transit bridge, archive, and profile mount. Replace the pilot source, destination, and DNS address with your own values.

```routeros
/container/mounts/add list=kp-awg-profile src=disk1/kp-awg-profile dst=/config
/container/add name=kp-awg file=keenproxy-awg-arm64.tar root-dir=disk1/kp-awg-root interface=kp-awg-veth mountlists=kp-awg-profile user=65534:65534 privileged=no logging=yes dns=192.0.2.1 cmd="-config /config/awg.conf -socks-listen 192.0.2.2:1080 -allow-from 192.0.2.1"
/container/start [find where name=kp-awg]
/ip/socksify/add name=kp-awg socks5-server=192.0.2.2 socks5-port=1080 disabled=no
/ip/firewall/nat/add chain=dstnat action=socksify protocol=tcp src-address=198.51.100.10 dst-address=203.0.113.10 dst-address-type=!local dst-port=443 socksify-service=kp-awg comment=kp-awg-pilot
```


See [RouterOS Container](https://manual.mikrotik.com/docs/containers/) and [RouterOS Socksify](https://manual.mikrotik.com/docs/network-management/socks/socksify/).
