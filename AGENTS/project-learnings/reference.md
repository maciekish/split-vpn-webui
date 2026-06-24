# Reference: unifi-split-vpn Scripts & Config Examples

The working split-VPN reference implementation lives at:
```
/Users/maciekish/Developer/Repositories/Appulize/unifi-split-vpn/
```

These are examples of what needs to be re-implemented in Go, **not runtime dependencies**.

Key files:
- `on_boot.d/20-ipset.sh` — domain routing via ipset/dnsmasq
- `on_boot.d/90-ipset-prewarm.sh` — DNS pre-warm pattern
- `on_boot.d/21-wg0.sgp.contoso.com-unit.sh` — how systemd VPN units are created on boot
- `systemd/units/` — example systemd unit files
- `sgp.contoso.com/wg0.conf` — real WireGuard client config (format reference)
- `web.contoso.com/DreamMachine.ovpn` — real OpenVPN client config (format reference)
- `sgp.contoso.com/vpn.conf`, `web.contoso.com/vpn.conf` — routing metadata files

## Policy Routing Setup Pattern

```bash
ipset create svpn_<group>_v4 hash:net family inet timeout 86400 -exist
ipset create svpn_<group>_v6 hash:net family inet6 timeout 86400 -exist
ipset create svpn_<group>_src_v4 hash:net family inet timeout 86400 -exist
ipset create svpn_<group>_src_v6 hash:net family inet6 timeout 86400 -exist

# dnsmasq entries for exact/wildcard domains
ipset=/<domain>/svpn_<group>_v4,svpn_<group>_v6
```

iptables rule pattern (use generation-chain approach in Go, not inline PREROUTING):
```bash
iptables  -t mangle -A SVPN_MARK -m set --match-set svpn_<group>_v4 dst -j MARK --set-mark <fwmark>
iptables  -t nat    -A SVPN_NAT  -m mark --mark <fwmark> -o <vpn_dev> -j MASQUERADE
ip rule add fwmark <fwmark> table <route_table> priority 100
# Same for ip6tables and ip -6 rule
```

**MASQUERADE rule is required** — without it, LAN source IPs exit the VPN tunnel and get dropped by the endpoint.

## Resolver Pattern

```bash
# Domain A/AAAA via DoH on a specific VPN interface
curl --interface <vpn_dev> -s \
    "https://cloudflare-dns.com/dns-query?name=<domain>&type=A" \
    -H "accept: application/dns-json"

# Wildcard subdomain discovery via CT logs
curl -s "https://crt.sh/?q=%25.<domain>&output=json"

# ASN prefix discovery via RIPE
curl -s "https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS<asn>"
```

- CNAME chaining: if response contains type=5, query CNAME target one level deep
- Wildcard mode: discovered subdomains are deduplicated, resolved, inserted into destination sets
- ASN mode: currently announced IPv4/IPv6 prefixes inserted and refreshed periodically

## WireGuard vpn.conf Reference

```ini
VPN_PROVIDER=external
DEV=wg0-sgp
ROUTE_TABLE=101
MARK=0x169
FORCED_IPSETS="svpn_sgp_v4:dst svpn_sgp_v6:dst"
VPN_BOUND_IFACE=br0
```

## OpenVPN vpn.conf Reference

```ini
VPN_PROVIDER=openvpn
DEV=tun0
ROUTE_TABLE=102
MARK=0x170
FORCED_IPSETS="svpn_web_v4:dst svpn_web_v6:dst"
VPN_BOUND_IFACE=br0
```

## WireGuard Parser Edge Cases

- **`Address` field**: whitespace-tolerant comma-separated CIDR list with mixed IPv4/IPv6 (e.g. `10.49.1.2 ,2001:db8:a161::2`)
- **`Table` directive**: if present in user config, register it with allocator (mark as used) rather than override
- **`PostUp`/`PreDown`/`PostDown`**: preserve user-supplied hooks; strip lines referencing peacey `updown.sh` and warn
- **`PresharedKey`**: store with `0600` permissions
- **Multiple `[Peer]` sections**: must be supported

## OpenVPN Parser Edge Cases

- **Inline blocks**: `<ca>`, `<cert>`, `<key>`, `<tls-crypt>`, `<tls-auth>` multi-line content
- **Separate credential files**: if `.ovpn` references external files via directives, those files must be uploaded separately
- **`dev` directive**: `dev tun` (no number) → assign specific interface name to avoid conflicts
