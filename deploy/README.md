# Production deployment

The Compose file runs mcproxy with PostgreSQL and Redis. Keep the admin panel on a separate SvelteKit 3+ server and expose only the panel's server-side proxy to operators.

1. Create a deployment `.env` (not committed) with at least `POSTGRES_PASSWORD` and `MCPROXY_ADMIN_TOKEN`.
2. Set `MCPROXY_GATE_FORWARDING_MODE` and its matching backend secret in `.env`.
3. Put the GeoLite2 database in the `/data` volume if GeoIP rules are enabled, or set `MCPROXY_GEOIP_PATH` to a mounted path.
4. Run `docker compose -f deploy/docker-compose.yml up -d --build`.
5. Allow TCP 25565 publicly. Keep port 8080 on an internal network or behind the SvelteKit server/reverse proxy.

The panel server should call `http://mcproxy:8080/api/v1` with `Authorization: Bearer <token>` from server-side code. Do not store the token in browser local storage or expose it in client JavaScript. If direct browser API calls are unavoidable, set `MCPROXY_ADMIN_ALLOWED_ORIGINS` to an exact HTTPS origin.

Before production, add TLS termination, firewall the Minecraft backends so only mcproxy can reach them, back up PostgreSQL, and configure Prometheus to scrape `/metrics` from an internal network.
