# mcproxy admin panel

Independent SvelteKit 3+ and TailwindCSS operations panel. It uses server-side requests to mcproxy so `MCPROXY_ADMIN_TOKEN` never reaches browser JavaScript.

## Run

```sh
cp .env.example .env
npm install
npm run dev
```

Required server-only variables:

- `MCPROXY_API_URL`: internal mcproxy HTTP URL
- `MCPROXY_ADMIN_TOKEN`: mcproxy admin token
- `ORIGIN`: public panel origin for SvelteKit CSRF checks and trusted URL generation
- `ADMIN_PANEL_USERNAME`: panel login username
- `ADMIN_PANEL_PASSWORD`: panel login password; use a secret manager in production
- `ADMIN_PANEL_SESSION_SECRET`: at least 32 random characters for signed httpOnly sessions

Each secret value also accepts a `*_FILE` variant for mounted Secret files: `MCPROXY_ADMIN_TOKEN_FILE`, `ADMIN_PANEL_USERNAME_FILE`, `ADMIN_PANEL_PASSWORD_FILE`, and `ADMIN_PANEL_SESSION_SECRET_FILE`. Direct environment variables take precedence.

Deploy behind an identity-aware reverse proxy or VPN as defense in depth. The panel provides login, health/statistics, backend routing visibility, per-server forwarding mode (`none`, `legacy`, `bungeeguard`, or `velocity`), policy/Geo/rule controls, server create/enable/disable/delete actions, and audit history. The mcproxy token remains server-only.

The included Docker image binds to `0.0.0.0:3000` and has a login-page healthcheck. Keep the container port private behind TLS termination and an identity-aware reverse proxy.

Panel login attempts are limited to 10 failures per client address per minute per panel instance. Configure the reverse proxy's trusted client-address handling correctly; for multi-instance deployments, enforce an additional shared rate limit at the proxy or identity provider.

Session logout is POST-only so a cross-site image or link cannot trigger a state-changing request.
