# mcproxy HTTP API reference

This document describes the control-plane API implemented by `internal/api/router.go`.
All `/api/v1/*` endpoints require either `Authorization: Bearer <MCPROXY_ADMIN_TOKEN>`
or `X-API-Token: <MCPROXY_ADMIN_TOKEN>`. Keep this API on a private network; the
separate SvelteKit panel calls it server-side and does not expose the token to browsers.

## Health and metrics

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/health` | No | Liveness only. |
| GET | `/ready` | No | Database, Redis fail-closed coordination, and Gate readiness. |
| GET | `/metrics` | No | Prometheus metrics; restrict it to the monitoring network. |

`/ready` returns `503` while a required dependency is unavailable. It must be used
for traffic readiness, while `/health` is suitable for process liveness.

## Read and write endpoints

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/v1/stats` | Process-lifetime counters and configured resource counts. |
| GET | `/api/v1/audits?limit=20&offset=0&event=...&actor=...&server_id=1` | Recent audit events with `items`, `has_more`, `limit`, and `offset`; `limit` and `offset` are bounded. `actor` performs a case-insensitive partial match. Repeat `server_id` for multiple scopes. |
| GET/POST | `/api/v1/servers/` | List or create backends. |
| GET/PUT/DELETE | `/api/v1/servers/{id}/` | Read, update, or delete one backend. |
| GET | `/api/v1/servers/{id}/health` | Perform a short TCP reachability check for the configured upstream; returns `healthy`, `unreachable`, or `disabled`. In the main process it also reports `gate_registered` and `gate_token_applied` so asynchronous Gate sync is visible. |
| POST | `/api/v1/servers/{id}/bungeeguard-token` | Rotate that backend's 32-character BungeeGuard token. |
| GET/PUT/DELETE | `/api/v1/policy/global` | Read or change the global threshold policy. |
| GET/PUT/DELETE | `/api/v1/policy/servers/{id}` | Read or change a backend policy override. |
| GET/PUT/DELETE | `/api/v1/policy/geo?server_id={id}` | Read or change global or backend Geo policy. Omit `server_id` for global scope. |
| GET/POST | `/api/v1/rules/?server_id={id}&limit=25&offset=0` | List or create allow/deny rules. GET returns `items`, `has_more`, `limit`, and `offset`; omit `server_id` to use global scope. |
| DELETE | `/api/v1/rules/{id}` | Delete a rule. |
| POST | `/api/v1/evaluate` | Evaluate an attempted connection against the current policy. |

The `event` audit filter accepts `connect.blocked`, `policy.update`, `geo.update`, `policy.reset`,
`server.create`, `server.update`, `server.delete`,
`server.bungeeguard_token.rotate`, `rule.create`, and `rule.delete`.

The router accepts both the documented trailing-slash collection paths and the
panel's normalized paths. JSON mutation bodies are limited to 1 MiB.

## Request examples

Create a backend:

```http
POST /api/v1/servers/ HTTP/1.1
Authorization: Bearer ${MCPROXY_ADMIN_TOKEN}
Content-Type: application/json

{"name":"survival","upstream":"10.0.0.12:25565","forwarding_mode":"bungeeguard","enabled":true}
```

To distribute a public route across multiple backends, create multiple enabled
servers with the same `public_host` (or `public_addr`). Gate selects among the
matching backends using a deterministic client-address hash; the source TCP
port is intentionally excluded so reconnects from one client remain stable.
For an unmatched public host, all enabled servers form the default pool—even
servers that have their own explicit public route—and use the same hash
distribution. If a selected backend is no longer registered, the next
available backend in that pool is used.

Create a server-scoped rule:

```http
POST /api/v1/rules/ HTTP/1.1
Authorization: Bearer ${MCPROXY_ADMIN_TOKEN}
Content-Type: application/json

{"kind":"ip-deny","target":"203.0.113.0/24","is_cidr":true,"reason":"abuse","server_id":1}
```

Evaluate without recording a new audit/counter event:

```json
{"server_id":1,"ip":"203.0.113.10","nickname":"Example","record":false}
```

Supported rule kinds are `ip-allow`, `ip-deny`, `name-allow`, `name-deny`,
`asn-allow`, and `asn-deny`. IP targets may be a single address or CIDR;
ASN targets accept `AS12345` and `12345`.

## BungeeGuard rotation

1. Read the backend from `GET /api/v1/servers/` and copy its exact
   `bungeeguard_allowed_token` into that backend's BungeeGuard
   `allowed-tokens:` list.
2. Rotate with `POST /api/v1/servers/{id}/bungeeguard-token` only when the
   backend configuration can be updated immediately.
3. Replace the old allowed token and reload BungeeGuard before accepting players.

Tokens are generated and stored per backend. A global BungeeGuard secret is not
supported and deployment preflight rejects the old global setting.

## Validation and security limits

- `upstream` and `public_addr` must be `host:port`; server names are limited to 128 runes.
- Rule targets are limited to 256 runes, reasons to 512 runes, and Geo lists to 256 entries.
- Thresholds are integers from 0 through 1,000,000.
- Admin authentication failures are throttled per source IP.
- Actor identity headers are trusted only when their HMAC matches
  `MCPROXY_ADMIN_ACTOR_SECRET`; otherwise audit records use `admin-api`.
- Configure `MCPROXY_TRUST_PROXY_HEADERS` only with explicit trusted proxy CIDRs.
- Do not expose `/metrics`, `/ready`, or the admin API directly to the public Internet.
