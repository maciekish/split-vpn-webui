# VPN Connection Inspector Plan

Date: 2026-02-24
Scope: Planning only (no code changes)

## Goal

Add a per-VPN “View Connections” action on each VPN card that shows active flows with:
- Source device MAC/IP:port
- Destination IP:port
- Protocol/state
- VPN association
- Domain hints from resolver/pre-warm cache
- Per-connection traffic totals and rate (when safe/available)

The design must prioritize routing stability and low CPU/memory overhead on UDM SE, including high-throughput environments (2 Gbps+).

## Constraints and Realities

1. The gateway’s primary job is packet forwarding, not deep telemetry.
2. Full packet capture is too expensive/risky for continuous operation.
3. Source MAC is not naturally preserved in conntrack tuples and must be derived.
4. Domain mapping from destination IP is probabilistic (CDN/shared-IP ambiguity).
5. We should avoid always-on heavy polling.

## Technical Approach

## 1) Flow Source of Truth: conntrack

Use conntrack snapshots/events as the base data source:
- `conntrack -L` / `conntrack -E` for active flow tuples/state.
- Track per-flow bytes/packets if conntrack accounting is enabled.

Why:
- Kernel already maintains conntrack state for NAT/firewall workflows.
- Lower overhead than packet mirroring/pcap.
- Gives structured tuples and counters suitable for UI summaries.

## 2) Reliable VPN Attribution: Persist MARK into Conntrack

Current routing marks (`MARK --set-mark`) identify policy decisions, but per-flow introspection is easier if marks are persisted in conntrack state.

Plan:
1. Ensure mangle chain includes `CONNMARK --save-mark` after mark decisions for new flows.
2. Ensure early `CONNMARK --restore-mark` where appropriate for established packets.
3. Use conntrack mark filtering (`-m/--mark`) to list only flows for a specific VPN mark.

Outcome:
- Deterministic flow-to-VPN mapping from existing policy marks.
- Fast per-VPN filtering without reconstructing policy logic in user space.

## 3) Source MAC Resolution (Best Effort)

conntrack tuples are L3/L4, so source MAC is derived:
1. Primary: map source IP to MAC from neighbor table (`ip neigh` for IPv4/IPv6).
2. Secondary: enrich from DHCP/lease inventory when available.
3. Fallback: show `unknown` if unresolved.

Notes:
- MAC for stale/remote devices may be unavailable.
- IPv6 temporary addresses may rotate frequently.
- UI should explicitly label MAC field as best-effort.

## 4) Domain Correlation (Hint, Not Ground Truth)

Map destination IPs to likely domains using existing caches:
1. Resolver cache (`resolver_cache`) direct selector mappings.
2. Pre-warm cache (`prewarm_cache`) via set-name to group/rule linkage.
3. Optional short-lived DNS observation map (`client_ip + answer_ip + ttl window`) for stronger recent correlations.

Display model:
- `domainHint`: first best match.
- `domainCandidates`: optional list when ambiguous.
- `confidence`: `high|medium|low`.

Rules:
- Never claim exact domain certainty for shared CDN IPs.
- Surface ambiguity in UI instead of hiding it.

## 5) Per-Connection Bandwidth/Traffic

Feasible with guardrails:
1. Read conntrack byte/packet counters.
2. Keep in-memory previous sample map keyed by canonical flow key.
3. Compute rx/tx delta per interval for instantaneous bps.

Prerequisite:
- `nf_conntrack_acct=1` (otherwise counters may be missing for flows created while disabled).

Fallback behavior:
- If accounting unavailable, still show active flows without throughput.
- Expose a clear status badge: `Flow counters unavailable (nf_conntrack_acct=0)`.

## Runtime Performance Strategy

## Collector Mode

1. Default: on-demand only (collector runs when user opens the connections drawer/modal).
2. Auto-stop when UI closes or after inactivity timeout.
3. Optional “pin live view” with explicit user opt-in.

## Sampling Strategy

1. Normal interval: 2s.
2. High-load interval: 5s when `nf_conntrack_count` crosses threshold.
3. Hard cap on rendered entries per VPN (e.g. top 200 by recent bytes/s).
4. Pagination/virtualized table for additional entries.

## Adaptive Degradation

When conntrack table is very large:
1. Disable domain enrichment first.
2. Then disable MAC resolution refresh frequency (use cached results).
3. Finally increase polling interval.

Never do:
- Full reverse DNS per flow.
- Per-packet processing in userspace.
- Continuous global polling for all VPNs with no viewer.

## Proposed API Design

## Endpoints

1. `GET /api/vpns/{name}/connections`
- Query params: `limit`, `cursor`, `sort`, `includeDomains`, `includeMac`.
- Returns current snapshot page and sampling metadata.

2. `GET /api/vpns/{name}/connections/status`
- Collector running state, sample interval, degraded flags, last error.

3. `POST /api/vpns/{name}/connections/start`
- Start on-demand collector for this VPN.

4. `POST /api/vpns/{name}/connections/stop`
- Stop collector.

5. Optional SSE event: `vpn-connections`
- Pushes incremental table updates while live view is open.

## Response Shape (Draft)

```json
{
  "vpn": "rbx.contoso.com",
  "interface": "wg-sv-rbxcontoso",
  "mark": "0x171",
  "sampleIntervalMs": 2000,
  "degraded": false,
  "rows": [
    {
      "id": "tcp:192.168.1.25:51742-142.250.74.14:443",
      "proto": "tcp",
      "state": "ESTABLISHED",
      "srcIp": "192.168.1.25",
      "srcMac": "00:11:22:33:44:55",
      "srcPort": 51742,
      "dstIp": "142.250.74.14",
      "dstPort": 443,
      "domainHint": "www.google.com",
      "domainConfidence": "low",
      "bytesIn": 4561221,
      "bytesOut": 882214,
      "bpsIn": 1450000,
      "bpsOut": 240000
    }
  ],
  "nextCursor": ""
}
```

## UI Plan

## VPN Card Changes

1. Add button: `Connections`.
2. Opens side panel/modal scoped to selected VPN.
3. Table columns:
- Source (MAC/IP:port)
- Destination (IP:port + domain hint)
- Protocol/State
- Current rate (down/up)
- Total bytes (down/up)

## UX Details

1. Always show clear “best effort” tags for MAC/domain fields.
2. Display collector health badge: `Live`, `Degraded`, `Paused`, `Error`.
3. Provide manual refresh and “stop live updates” controls.
4. If counters unavailable, keep table functional and hide bps columns.

## Internal Package Plan

## New package

`internal/connections/`

Responsibilities:
1. conntrack collection/parsing.
2. flow key normalization + delta calculator.
3. VPN mark filtering.
4. MAC/domain enrichment orchestrator.
5. runtime budgeting and degradation control.

Suggested files:
- `collector.go` (lifecycle, scheduler, backpressure)
- `conntrack.go` (snapshot/event command wrappers + parser)
- `enrich_mac.go` (neighbor/lease lookup)
- `enrich_domain.go` (resolver/prewarm cache mapping)
- `rates.go` (delta and EWMA smoothing)
- `api_types.go` (DTOs for handlers/SSE)

## Database Changes (Minimal)

Baseline plan requires no persistent flow history table.

Optional future table if short historical charts are desired:
- `connection_samples` (time-windowed aggregate only, not raw per-packet/per-flow long-term retention).

Default: keep runtime-only in memory to reduce write amplification.

## Implementation Phases

1. Kernel integration + mark persistence
- Add/validate CONNMARK save/restore in routing rule application.
- Add verification checks in health diagnostics.

2. Collector MVP
- Per-VPN snapshot endpoint without enrichment.
- On-demand lifecycle and hard caps.

3. Enrichment
- Add MAC map layer.
- Add domain hint layer with confidence labels.

4. Throughput
- Add per-flow delta rates when conntrack accounting is available.
- Add degrade controls and UI indicators.

5. Hardening
- Load test with synthetic conntrack volume.
- Validate no measurable routing impact under expected home/SMB traffic profiles.

## Acceptance Criteria

1. Opening one VPN connection view does not disturb routing or tunnel stability.
2. Collector is off by default and auto-stops when not viewed.
3. At least 95% of active flows show source IP and destination tuple correctly.
4. MAC/domain enrichment degrades gracefully without breaking base flow visibility.
5. On high conntrack load, system auto-degrades before CPU/memory pressure becomes risky.
6. Errors are surfaced clearly in API/UI (`{"error":"..."}` envelope).

## Risks and Mitigations

1. conntrack table too large
- Mitigation: adaptive intervals, top-N caps, disable expensive enrichment first.

2. Ambiguous domain attribution
- Mitigation: confidence labels + candidate list, never absolute claims.

3. Missing MAC mappings
- Mitigation: best-effort label + cached neighbor snapshots.

4. Counter availability mismatch
- Mitigation: detect `nf_conntrack_acct`, show explicit partial-feature state.

## Research References

- Linux conntrack sysctls (`nf_conntrack_count`, `nf_conntrack_max`, `nf_conntrack_acct`, events): <https://www.kernel.org/doc/html/latest/networking/nf_conntrack-sysctl.html>
- conntrack command options (including mark filtering): <https://conntrack-tools.netfilter.org/manual.html>
- iptables CONNMARK semantics: <https://www.man7.org/linux/man-pages/man8/iptables-extensions.8.html>
- dnsmasq `--ipset` matching behavior for domains/subdomains: <https://dnsmasq.org/docs/dnsmasq-man.html>
