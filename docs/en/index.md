# Your network, your routes

![Ordo Systems](../assets/ordo-systems-logo.png){ width="440" }

KeenProxy is an experimental routing application for compatible Keenetic/Entware environments. It lets you send selected domains, addresses, countries, networks, or devices through a connection, connect directly, or block them. The web interface is the control panel; the router daemon applies the configuration and owns network effects.

## Start here

- **Explore first:** open the [browser demo](browser-demo.md). No router or credentials are required.
- **Prepare a router:** read [Getting started](getting-started.md) before installing anything.
- **Learn the workflow:** follow the [illustrated user guide](user-guide.md).
- **Understand policy:** read [Routing and DNS](routing.md).

## The core idea

A **connection** is a named WireGuard, link-based, sing-box, or TrustTunnel profile. A **group** connects domains, addresses, or country selectors to a routing target. **Networks and clients** choose which devices use those rules. The **Overview, Journal, and Diagnostics** screens help explain the applied state.

All screens edit one shared draft. Editing a connection or a group does not immediately reconfigure the router. Review your changes and choose **Apply**; **Discard** returns to the last saved configuration.

!!! warning "Experimental software"
    Compatibility depends on architecture, firmware, kernel capabilities, and build features. Synthetic demo results do not prove connectivity, DNS protection, or compatibility with your router.
