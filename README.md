mcproxy — Minecraft Security Proxy built on Minekube Gate

Language: 한국어 문서는 [README.ko.md](README.ko.md)

Overview

mcproxy is a security-focused proxy for Minecraft (Java Edition) built on top of Minekube Gate (Go). It adds a management plane via HTTP API and data-plane protections such as IP/nickname DoS throttling and GeoIP-based country blocking, with per-server and global thresholds, backed by entgo ORM with SQLite (default) or PostgreSQL.

Key Features

- Built on Minekube Gate (Go)
- Management plane: HTTP API for runtime policy control (block/allow, thresholds, GeoIP rules)
- DoS-suspect throttling by IP and nickname (global and per-server)
- GeoLite2 (City) database–based country allow/deny
- Per-server configuration of thresholds and policies
- entgo ORM with SQLite or PostgreSQL

Status

- WIP: Initial design and API surface are defined in [README.ko.md](README.ko.md). Implementation will follow.

Quick Links

- Full spec and API (Korean): [README.ko.md](README.ko.md)
- License: [LICENSE.md](LICENSE.md)

License

AGPL-3.0. See [LICENSE.md](LICENSE.md).

