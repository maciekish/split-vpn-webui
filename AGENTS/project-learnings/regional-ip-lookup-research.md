# Regional IP Lookup Research (No Heavy API Dependence)

Date: 2026-02-24
Scope: Research only (no code changes)

## Objective

Find a robust way to discover region-specific IP answers for domains without depending on a single rate-limited third-party API.

## Short Answer

Yes. The most solid approach is to run a small set of your own regional DNS probes/resolvers and query them directly over DNS/DoH/DoT. This avoids third-party API quotas as a hard dependency and gives predictable behavior.

## Key Findings

1. ECS can help simulate regional lookups, but it is not universal.
2. Cloudflare 1.1.1.1 explicitly does not send ECS, so it cannot be your ECS engine.
3. ECS has cache/privacy/operational caveats and should be scoped carefully.
4. Probe networks like RIPE Atlas are useful but quota/credits limited.
5. Bulk/passive DNS datasets can reduce live-query volume but are not real-time and still come with access controls.

## Option Analysis

## Option A (Recommended): Self-hosted Regional Resolver Probes

Description:
- Deploy lightweight resolver/proxy nodes in target regions (for example: EU, US-East, US-West, APAC).
- Query those nodes directly for A/AAAA/CNAME expansion.
- Keep per-region caches and TTL-respectful refresh logic.

Why this is the best fit:
- No third-party API request quota bottleneck for day-to-day operation.
- Regional view is real and controlled by your own infrastructure.
- Easy to add/remove regions by adding/removing nodes.
- Can be run with strict query budgets and local caching.

Tradeoffs:
- You own uptime/ops for those probe nodes.
- Requires secure transport/auth between gateway and probes.

## Option B (Secondary): ECS-Based Queries via Public Resolver

Description:
- Use a resolver/API that supports ECS, and request answers for chosen client subnets.

Pros:
- Fast to bootstrap.
- Good fallback for “spot checks”.

Cons:
- Not all resolvers support ECS behavior needed for this.
- ECS behavior depends on authoritative DNS support.
- ECS can increase cache complexity and has known security/privacy caveats.

Best use:
- Fallback path, not the primary production mechanism.

## Option C: External Measurement Platforms (RIPE Atlas etc.)

Description:
- Use distributed probe platforms for DNS measurements.

Pros:
- Broad geographic reach with minimal infra setup.

Cons:
- Credits/quotas and measurement limits are built into platform economics.
- Less deterministic for continuous production pipelines.

Best use:
- Validation and spot diagnostics.

## Option D: Bulk Passive DNS/Open Datasets

Description:
- Use datasets like Rapid7 FDNS as enrichment for candidate IPs/subdomains.

Pros:
- Low live-query pressure.
- Good breadth.

Cons:
- Not real-time.
- Access controls and ingestion complexity.
- Requires quality scoring before using for routing decisions.

Best use:
- Optional enrichment layer only.

## Recommended Architecture (Pragmatic Hybrid)

1. Primary path: Self-hosted regional resolver probes.
2. Fallback path: ECS query where needed.
3. Enrichment path: Bulk dataset ingestion for candidate expansion.
4. Runtime selection: Prefer freshest, highest-confidence answer set per region.

## Proposed Data Model (for future implementation)

1. Region catalog
- `region_id` (string, e.g. `us_east`, `eu_central`)
- `resolver_endpoint` (dns/doh endpoint)
- `health_state`

2. Resolution cache
- `domain`
- `region_id`
- `family` (`inet`/`inet6`)
- `ip_or_cidr`
- `observed_at`
- `ttl_expires_at`
- `source` (`probe`, `ecs`, `dataset`)
- `confidence` (0-100)

3. Run audit
- `run_id`
- `region_id`
- `domains_total`, `domains_ok`, `domains_failed`
- `duration_ms`
- `error_summary`

## Reliability Guardrails

1. Never hard-replace with an empty run result.
2. Use additive updates with expiry windows (already aligned with existing 24h cache strategy).
3. Apply per-region query budgets and concurrency limits.
4. Keep stale-but-valid answers until replacement arrives or TTL/retention expires.
5. Mark low-confidence data (dataset-only) so policy can ignore it by default.

## Decision

Use self-hosted regional probes as the default production design. Keep ECS and dataset inputs as optional augmentations, not hard dependencies.

## References

- Google Public DNS JSON DoH API (`edns_client_subnet`): <https://developers.google.com/speed/public-dns/docs/doh/json>
- Google ECS guidance: <https://developers.google.com/speed/public-dns/docs/ecs>
- Cloudflare 1.1.1.1 FAQ (no ECS): <https://developers.cloudflare.com/1.1.1.1/faq/>
- RFC 7871 (ECS): <https://www.rfc-editor.org/rfc/rfc7871.html>
- ISC note on ECS in BIND variants: <https://kb.isc.org/docs/edns-client-subnet-ecs-for-resolver-operators-getting-started>
- Unbound ECS module docs (`subnetcache`): <https://www.nlnetlabs.nl/documentation/unbound/unbound.conf/>
- Unbound ECS security advisory context: <https://nlnetlabs.nl/projects/unbound/security-advisories/>
- RIPE Atlas credits/quota model: <https://atlas.ripe.net/docs/getting-started/credits.html>
- RIPE Atlas user-defined measurement quotas: <https://atlas.ripe.net/docs/getting-started/user-defined-measurements.html>
- Rapid7 Open Data overview: <https://opendata.rapid7.com/>
- Rapid7 FDNS dataset page: <https://opendata.rapid7.com/sonar.fdns_v2/>
