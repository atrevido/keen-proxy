# Your network, your routes

![KeenProxy logo](../assets/logo.png){ width="128" style="vertical-align: middle" } &nbsp; ![Ordo Systems](../assets/ordo-systems-logo.png){ width="160" style="vertical-align: middle" }

KeenProxy is an experimental routing application for compatible Keenetic/Entware environments. It lets you send selected domains, addresses, countries, networks, or devices through a connection, connect directly, or block them. The web interface is the control panel; the router daemon applies the configuration and owns network effects.

## Start here

- **Explore first:** open the [browser demo](browser-demo.md). No router or credentials are required.
- **Prepare a router:** read [Getting started](getting-started.md) before installing anything.
- **Learn the workflow:** follow the [illustrated user guide](user-guide.md).
- **Understand policy:** read [Routing and DNS](routing.md).
- **Manage releases:** see [updates and rollback from the panel](getting-started.md#update-from-the-panel).

## The core idea

A **connection** is a named WireGuard, link-based, sing-box, or TrustTunnel profile. A **group** connects domains, addresses, or country selectors to a routing target. **Networks and clients** choose which devices use those rules. The **Overview, Journal, and Diagnostics** screens help explain the applied state.

Routing settings share one draft. Review changes to connections or rules and choose **Apply**. **Discard** returns to the last saved configuration. Release installation and scheduling use their own controls under **Settings**.

!!! warning "Experimental software"
    Compatibility depends on architecture, firmware, kernel capabilities, and build features. Synthetic demo results do not prove connectivity, DNS protection, or compatibility with your router.
