# Multi-region operations

The Singapore control plane remains authoritative. Regional nodes make only
outbound HTTPS requests to `/api/v2/node/*`, authenticate with a per-node mTLS
certificate plus a bearer secret, and never expose a management listener.

## Kuala Lumpur (`MY`, `my-kul-01`)

- Endpoint: `47.250.164.136:53147`
- Tunnel networks: `10.67.0.0/24`, `fd67:67:67::/64`
- Exit mode: `ipv4_exit_ipv6_blocked`
- Probe: `https://my-kul-01.vpn.tnestai.asia/latency`

IPv4 is masqueraded through the node. IPv6 is routed into WireGuard by every
client (`::/0`) and explicitly dropped on the node; NAT66 is not configured.
This is deliberate leak prevention, not an IPv6 exit.

Install or renew the dedicated HTTPS probe after DNS is in place:

```bash
sudo scripts/remote/install-latency-endpoint.sh \
  my-kul-01.vpn.tnestai.asia \
  config/regions/my-kul-01/nginx-latency.conf
```

The installer backs up any same-name Nginx site, uses the ACME webroot flow,
validates the final configuration and rolls back the site on failure. The
probe returns `204 No Content`, has no response body or cookie, and must be
deployed as DNS-only rather than through a reverse proxy so measured latency
reaches the node itself.

Keep the `MY` region and `my-kul-01` node disabled until all of these checks
pass:

1. DNS resolves the probe name to `47.250.164.136` and its TLS certificate is
   valid.
2. The cloud security group allows `53147/udp`, `443/tcp`, required ICMP and
   outbound traffic. SSH is limited to the Bangkok management source.
3. The node agent validates desired state in read-only mode without logging
   key material.
4. A dedicated test device confirms Kuala Lumpur IPv4 egress, tunnel DNS,
   failed public IPv6, restart recovery and firewall rollback.
5. Existing Singapore peers, usage history and admin access remain unchanged.

The node agent keeps ten local `wg showconf` rollback points, applies peer
changes with `wg syncconf`, and restores the last snapshot if an apply fails.
Never paste the API token, client certificate private key, WireGuard private
key, client private keys or PSKs into chat or logs.
