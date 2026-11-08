# Third-party software and data

KeenProxy's GPL-3.0-only declaration applies to its original code. Dependencies, separately installed executables, and imported data retain their own terms. This file records important distinctions; it is not an exhaustive license-compatibility certification.

## Software dependencies

- **sing-box**: embedded engine from [SagerNet](https://github.com/SagerNet/sing-box), licensed under GPL-3.0-or-later by its upstream copyright holders. The pinned upstream file also contains an additional naming/association notice. Its complete notice is preserved in [LICENSES/sing-box.txt](LICENSES/sing-box.txt); retain it and account for the combined distribution when shipping binaries. This description does not certify all compatibility questions.
- **AmneziaWG client**: the optional ARM64 sidecar uses [amneziawg-go](https://github.com/amnezia-vpn/amneziawg-go) under the [MIT license](LICENSES/amneziawg-go.txt) and [gVisor](https://gvisor.dev/) under the [Apache License 2.0](LICENSES/gvisor.txt).
- **Other Go modules**: versions are recorded in `go.mod` and `go.sum`, including separate modules under `tools/`. Consult each upstream license and preserve required notices in source/binary distributions.
- **Frontend dependencies**: versions are recorded in `web/package.json` and `web/package-lock.json`. Their upstream licenses and notices still apply to the embedded compiled frontend.
- **TrustTunnel client**: optional external executable, downloaded from the upstream release selected in `tools/install.sh`. The installer retains the upstream license under the installation prefix. Its license is separate from KeenProxy's; verify the exact release's terms before redistributing it in a bundle.

## Country databases

Country databases are downloaded separately on the router, are approximate, and are not relicensed as GPL by this project. The built-in sources and their terms:

- **DB-IP IP to Country Lite** (enabled by default): [download page](https://db-ip.com/db/download/ip-to-country-lite), licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). Attribution, shown in the web interface while the database is in use: [IP Geolocation by DB-IP](https://db-ip.com/).
- **ip-location-db `user-country`** (disabled by default): [sapics/ip-location-db](https://github.com/sapics/ip-location-db) GitHub release, published under the [PDDL 1.0](https://opendatacommons.org/licenses/pddl/1-0/). Upstream notes that it aggregates inputs, some of which follow RIR terms.
- **Loyalsoldier GeoIP `Country-without-asn.mmdb`** (disabled by default): [Loyalsoldier/geoip](https://github.com/Loyalsoldier/geoip) latest GitHub release, built from MaxMind GeoLite2 data. MaxMind's GeoLite2 terms apply; the interface shows "Includes GeoLite2 data created by MaxMind" while it is in use. Review those terms before enabling it.

- **runetfreedom category databases** (disabled by default): `Country-ru-only.mmdb` (labels such as `RU-BLOCKED` and `RE-FILTER`) and `Country-asn.mmdb` (service networks such as `TELEGRAM`) from the latest [runetfreedom/russia-blocked-geoip](https://github.com/runetfreedom/russia-blocked-geoip) GitHub release. The repository is GPL-3.0. Its data comes from antifilter.download, community.antifilter.download and re:filter, whose own terms apply.

Custom databases added by URL or local file have their own terms.

## Fallback root certificates

`golang.org/x/crypto/x509roots/fallback` embeds Mozilla's root certificate set. It is used only when the router provides no system CA bundle. The module is BSD-licensed; the certificate data originates from the Mozilla NSS project.

## Domain presets and filter lists

The embedded community preset snapshot is derived from an exported domain collection. The catalog stores domain collections and display metadata; it does not contain upstream license grants. A collection being community-maintained does not, by itself, establish a redistribution license. Do not assume that applying GPLv3 to KeenProxy relicenses third-party lists. Before public redistribution, establish the applicable license/permission or replace/remove data whose terms cannot be confirmed.

DNS blocklists are fetched from separately configured providers. Each provider's license and attribution requirements apply to its content. The ability to download a list is not a license grant.
