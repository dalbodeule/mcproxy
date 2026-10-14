# Security

mcproxy is a security-focused Minecraft Java proxy. The data plane and the
management plane have different trust boundaries; exposing the management API
or metrics endpoint to the public Internet is not supported.

## Security boundaries

- Minecraft clients may reach only the Gate listener. The HTTP API and
  `/metrics` should remain on an internal network or behind an authenticated
  operator network.
- The admin panel is a separate SvelteKit application. Its mcproxy token,
  session secret, membership database, and MFA secrets are server-side only.
- Set the same high-entropy `MCPROXY_ADMIN_ACTOR_SECRET` in mcproxy and the
  panel when audit records must preserve member identity. The panel signs the
  actor header with HMAC; unsigned or invalid headers are recorded as
  `admin-api`.
- BungeeGuard credentials are generated and stored per backend. A global
  BungeeGuard secret is intentionally rejected by preflight checks.
- PostgreSQL and Redis are the production multi-instance path. Redis is
  fail-closed by default in the Compose and Kubernetes production templates;
  choosing fail-open is an explicit availability trade-off.

## Required production controls

Before exposing a deployment, run both preflight checks with real secret files
and run the migration/readiness sequence described in `deploy/README.md`:

```sh
./deploy/preflight.sh
./deploy/panel-preflight.sh
```

Also verify all of the following in the target environment:

- TLS terminates in front of the admin panel and `ORIGIN` is the exact public
  origin.
- The Minecraft backends accept traffic only from mcproxy and the forwarding
  mode/token is configured per backend.
- PostgreSQL uses TLS verification when it crosses a host or trust boundary.
- Redis uses authentication and TLS when it crosses a host or trust boundary.
- SecretStore, egress CIDRs, load-balancer source-IP preservation, and
  Prometheus thresholds are replaced with provider-specific values; the
  checked-in examples are not production values.

Set `MCPROXY_REQUIRE_POSTGRES_TLS=true` in the deployment preflight to reject
DSNs without an explicit `sslmode=require`, `verify-ca`, or `verify-full`.
For an external Redis deployment, set `MCPROXY_REQUIRE_REDIS_TLS=true` to
require `MCPROXY_REDIS_TLS=true` as well.
- Backups are encrypted and access-controlled, and a restore rehearsal has
  succeeded before a migration or image rollout.

## Implemented protections

- HTTP admin authentication, signed httpOnly sessions, session revocation,
  invitation-based membership, RBAC/server scopes, TOTP MFA, audit events, and
  login/body/rule input limits.
- Same-origin mutation checks, security headers, no-store responses for
  sensitive panel pages, internal-only return redirects, and API path/scope
  allowlists.
- Per-IP login reservation limits, policy counters, Redis coordination,
  fail-closed readiness, graceful drain, and low-cardinality Prometheus
  metrics/alerts.
- Immutable/pinned container bases, dependency vulnerability checks, image
  scanning hooks, migration backups/checksums, and deployment preflight smoke
  tests.

## Known limits

mcproxy does not replace provider-level L4 DDoS protection, SYN-flood
mitigation, conntrack capacity planning, or an identity provider. The optional
firewall examples are starting points only. The admin panel intentionally uses
one replica with SQLite until a shared membership database adapter is added.
Provider-specific SecretStore and egress configuration must be reviewed by the
operator; no repository-only test can prove those external controls.

## Reporting

Do not put credentials, tokens, database files, or an exploitable proof of
concept in a public issue. Report privately through the repository's configured
security advisory channel, including the affected version, deployment mode,
reproduction steps, and impact. Rotate any credential that appeared in logs or
an issue immediately.
