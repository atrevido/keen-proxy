---
title: KeenProxy
hide:
  - navigation
  - toc
---

<div class="kp-landing" markdown>

<section class="kp-hero" markdown>
<div class="kp-hero__text" markdown>

![KeenProxy logo](../assets/logo.png){ .kp-hero__logo width="72" }

# Your network, your routes

Selective traffic routing for Keenetic and Netcraze routers running Entware. Choose which devices, domains, addresses, and countries use a tunnel, connect directly, or are blocked — from one web panel.

<div class="kp-actions" markdown>
[:material-play: Try the demo](demo/){ .md-button .md-button--primary }
[Get started](getting-started.md){ .md-button }
</div>

</div>

[![KeenProxy web panel overview with synthetic traffic](../assets/screenshots/readme-overview.png){ .kp-hero__shot }](demo/)

</section>

<section class="kp-section" markdown>

## What it does

<div class="kp-grid" markdown>

<div class="kp-card" markdown>
:material-devices:{ .kp-card__icon }

### Device and network policies

Split routing, full tunneling, or direct access per network, with per-device overrides by MAC address.
</div>

<div class="kp-card" markdown>
:material-earth:{ .kp-card__icon }

### Domain, IP, and country groups

Route by domain, CIDR, or country — including exclusions such as “everything except Russia”.
</div>

<div class="kp-card" markdown>
:material-vpn:{ .kp-card__icon }

### Many connection types

WireGuard, sing-box connection links, raw sing-box outbounds, and an optional TrustTunnel client.
</div>

<div class="kp-card" markdown>
:material-shield-sync:{ .kp-card__icon }

### Failover

Fall back to another connection when one goes down, and return after it has recovered.
</div>

<div class="kp-card" markdown>
:material-dns:{ .kp-card__icon }

### DNS and blocklists

A local DNS frontend with DoH/DoT resolvers, selectable DNS routing, and DNS-only blocklists.
</div>

<div class="kp-card" markdown>
:material-file-document-check-outline:{ .kp-card__icon }

### Review before Apply

Every change goes into one shared draft. Apply it when ready, or discard it; journals explain the result.
</div>

</div>

</section>

<section class="kp-section" markdown>

## How it works

<div class="kp-steps" markdown>

<div class="kp-step" markdown>
### Add a connection

Import a WireGuard file or a connection link, or use the wizard.
</div>

<div class="kp-step" markdown>
### Define groups

Collect domains, addresses, or countries and pick where they go.
</div>

<div class="kp-step" markdown>
### Choose devices and Apply

Select which networks and clients follow the rules, then apply the draft.
</div>

</div>

</section>

<section class="kp-section" markdown>

## A look at the panel

<div class="kp-gallery" markdown>

<figure markdown>
[![Domain groups, targets, and address observations](../assets/screenshots/en-rules.png)](user-guide.md#3-add-a-routing-group)
<figcaption>Rules</figcaption>
</figure>

<figure markdown>
[![Connection profiles and routing targets](../assets/screenshots/en-connections.png)](user-guide.md#2-configure-a-connection)
<figcaption>Connections</figcaption>
</figure>

<figure markdown>
[![Diagnostics for a selected connection](../assets/screenshots/en-diagnostics.png)](user-guide.md#7-verify-the-result)
<figcaption>Diagnostics</figcaption>
</figure>

</div>

</section>

<section class="kp-cta" markdown>

## Try it in your browser

The demo runs the real web panel with synthetic data. No router or credentials are required.

<div class="kp-actions" markdown>
[:material-play: Open the demo](demo/){ .md-button .md-button--primary }
[Read the overview](overview.md){ .md-button }
</div>

<p class="kp-note">KeenProxy is experimental. Compatibility depends on the router architecture, firmware, kernel capabilities, and build features.</p>

</section>

</div>
