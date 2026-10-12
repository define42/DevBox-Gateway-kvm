# Gateway edge rate limiting

The gateway's separate unauthenticated connection budget, per-source limits,
absolute authentication deadline, and accept-rate limits bound application work.
They cannot prevent a TCP SYN flood or protect an overloaded upstream link.
Use a source-preserving network firewall in front of an Internet-facing gateway;
use your provider's upstream filtering for volumetric attacks.

## Opt-in Linux host example

[deploy/gateway-rate-limit.nft](../deploy/gateway-rate-limit.nft) is an additive
nftables example for the shared HTTPS/RDP TCP port. It does not flush the
existing firewall or change which ports are otherwise permitted. It drops only
over-budget initial SYN packets to the configured destination and interface;
ordinary established-session traffic is not rate-limited. SYN retransmissions
also consume tokens.

The `prerouting` hook at priority `-300` runs before connection tracking and
destination NAT. This lets one rule cover a native listener and a rootful Docker
published port while seeing the source address and destination port before
translation. It does not replace existing INPUT/FORWARD policy. See the
[Netfilter hook ordering](https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks),
[nftables manual](https://netfilter.org/projects/nftables/manpage.html), and
[Docker's original-destination guidance](https://docs.docker.com/engine/network/firewall-iptables/#match-the-original-ip-and-ports-for-requests).

Edit all four definitions before installing:

- `gateway_interface`: the external interface receiving client traffic, not the
  Docker bridge. Add equivalent rules for every exposed ingress interface.
- `gateway_ipv4` and `gateway_ipv6`: destination addresses as seen by that host
  on ingress. Behind a cloud/router NAT, these may be private addresses. Remove
  a family's matching rule if it is unused; cover every published address if
  there are several. The sample's documentation addresses provide no protection.
- `gateway_port`: the public host port, such as `GATEWAY_PORT=8443` in Compose or
  `443` for ACME TLS-ALPN-01, even when Docker forwards it to container port 8443.

The rules first allow 16 SYN/second with a 32-packet burst per IPv4 address or
IPv6 `/64`, grouping IPv6 privacy addresses within a prefix. Only SYNs passing
that source meter reach the shared 256 SYN/second bucket with a 512-packet burst,
so one over-limit source cannot drain the shared budget. Each source meter holds
at most 32,768 entries and expires idle entries after one minute. A failed meter
insertion falls through to the final drop. Successful source checks use `goto`
so the shared bucket returns to `prerouting`, skipping that fallback drop.
These are starting values, not capacity measurements. Tune them alongside the
[application limits](configuration.md), allowing headroom for SYN retransmissions,
browsers opening several connections, clients sharing NAT, and certificate
validation. Keep the global limit: source rotation can evade source-only limits.
The meter mechanism follows the official
[nftables meter examples](https://wiki.nftables.org/wiki-nftables/index.php/Meters).

Review and validate the edited file on the deployment host before applying it.
`--check` validates without committing firewall changes and normally requires
root/CAP_NET_ADMIN. Keep an out-of-band administrative session available:

```bash
sudo nft --check --file deploy/gateway-rate-limit.nft
sudo nft --file deploy/gateway-rate-limit.nft
sudo nft list table inet gokvm_gateway_edge
```

The first command in the file refuses installation if its dedicated table already
exists, avoiding duplicated rules on reload. Manage later replacements through
your firewall configuration system as an atomic transaction; do not repeatedly
load this file or flush the host ruleset. Integrate the reviewed rules with that
system's boot-time configuration so they survive a reboot. To remove only this
example's protection:

```bash
sudo nft delete table inet gokvm_gateway_edge
```

No firewall rules are automatically installed by the gateway or Compose.

## Verify the deployed path

From a separate client, verify HTTPS login, RDP authentication, and any enabled
ACME challenge path. Inspect the table's counters while generating a controlled
connection burst, and verify that an existing RDP session remains usable. Test
both IP families and every public address. Local loopback traffic does not test
the configured external interface. Do not assume an INPUT-only firewall covers
Docker's forwarded connections.

This example does not terminate TLS, proxy TCP, or rewrite source addresses. The
gateway deliberately keys its own limits from the socket peer, not forwarding
headers. A source-NATing load balancer or rootless container proxy can collapse
many clients into one peer; enforce source limits before that translation and
size the gateway's per-peer limits for the actual network path. A generic HTTP
reverse proxy also cannot carry the shared raw RDP protocol.

Distributed clients can still exhaust a finite new-connection budget, and
spoofed SYN traffic can consume the network bucket. These rules are resource
bounds, not a guarantee of new-client availability during a distributed attack.
Use upstream filtering, network allowlists or VPN access where appropriate, and
retain the gateway's application-level limits for completed but idle connections.
