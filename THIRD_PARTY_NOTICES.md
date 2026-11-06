# Third-party software and data

KeenProxy's GPL-3.0-only declaration applies to its original code. Dependencies, separately installed executables, and imported data retain their own terms. This file records important distinctions; it is not an exhaustive license-compatibility certification.

## Software dependencies

- **sing-box**: embedded engine from [SagerNet](https://github.com/SagerNet/sing-box), licensed under GPL-3.0-or-later by its upstream copyright holders. The pinned upstream file also contains an additional naming/association notice. Its complete notice is preserved in [LICENSES/sing-box.txt](LICENSES/sing-box.txt); retain it and account for the combined distribution when shipping binaries. This description does not certify all compatibility questions.
- **Other Go modules**: versions are recorded in `go.mod` and `go.sum`, including separate modules under `tools/`. Consult each upstream license and preserve required notices in source/binary distributions.
- **Frontend dependencies**: versions are recorded in `web/package.json` and `web/package-lock.json`. Their upstream licenses and notices still apply to the embedded compiled frontend.
- **TrustTunnel client**: optional external executable, downloaded from the upstream release selected in `tools/install.sh`. The installer retains the upstream license under the installation prefix. Its license is separate from KeenProxy's; verify the exact release's terms before redistributing it in a bundle.

## Country database

The default download is [DB-IP IP to Country Lite](https://db-ip.com/db/download/ip-to-country-lite), licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). Attribution: [IP Geolocation by DB-IP](https://db-ip.com/). The data is downloaded separately, is approximate, and is not relicensed as GPL by this project. Alternative databases have their own terms.

## Domain presets and filter lists

The embedded community preset snapshot is derived from an exported domain collection. The catalog stores domain collections and display metadata; it does not contain upstream license grants. A collection being community-maintained does not, by itself, establish a redistribution license. Do not assume that applying GPLv3 to KeenProxy relicenses third-party lists. Before public redistribution, establish the applicable license/permission or replace/remove data whose terms cannot be confirmed.

DNS blocklists are fetched from separately configured providers. Each provider's license and attribution requirements apply to its content. The ability to download a list is not a license grant.
