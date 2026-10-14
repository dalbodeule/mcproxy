.PHONY: all tools ent tidy build migrate migration-smoke postgres-restore-smoke panel-backup-smoke image-scan image-hardening-smoke security-audit supply-chain-check config-doc-check config-doc-check-smoke preflight preflight-smoke panel-preflight panel-preflight-smoke kubernetes-production-check kubernetes-production-check-smoke kubernetes-rollout-rehearsal firewall-check promtool-check run test vet race check panel-check validate-deploy release-check docker-images gate-e2e compose-smoke panel-smoke panel-dashboard-smoke panel-membership-smoke panel-migration-smoke panel-security-smoke panel-rehearsal ops-rehearsal clean

BIN := bin/mcproxy
PKG := ./cmd/mcproxy
GOCACHE ?= /tmp/mcproxy-gocache
GOENV := GOCACHE=$(GOCACHE)
GO_TEST_TIMEOUT ?= 5m
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS := -s -w -X mcproxy/internal/buildinfo.Version=$(VERSION) -X mcproxy/internal/buildinfo.Commit=$(COMMIT) -X mcproxy/internal/buildinfo.BuildTime=$(BUILD_TIME)

all: build

tools:
	$(GOENV) GO111MODULE=on go install entgo.io/ent/cmd/ent@latest

# Generate ent code from schema
ent:
	@test -d internal/ent/schema || mkdir -p internal/ent/schema
	@if ls internal/ent/schema/*.go >/dev/null 2>&1; then \
		$(GOENV) go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,privacy,lock,sql/schemaconfig ./internal/ent/schema; \
	else \
		echo "[ent] no schema .go files, skipping codegen"; \
	fi

tidy:
	$(GOENV) go mod tidy

build: ent tidy
	$(GOENV) go build -trimpath -ldflags='$(GO_LDFLAGS)' -o $(BIN) $(PKG)

migrate: build
	$(BIN) migrate

migration-smoke:
	docker build --pull=false -t mcproxy:migration-smoke --build-arg MCPROXY_VERSION="$(VERSION)" --build-arg MCPROXY_COMMIT="$(COMMIT)" --build-arg MCPROXY_BUILD_TIME="$(BUILD_TIME)" .
	docker run --rm -e MCPROXY_DB_DRIVER=sqlite -e MCPROXY_SQLITE_PATH=/data/migration-smoke.db mcproxy:migration-smoke migrate

postgres-restore-smoke:
	./deploy/postgres-restore-smoke.sh

panel-backup-smoke:
	@set -eu; tmpdir=$$(mktemp -d); trap 'rm -rf "$$tmpdir"' EXIT INT TERM; source="$$tmpdir/source.db"; target="$$tmpdir/target.db"; tampered="$$tmpdir/tampered.db"; backup_dir="$$tmpdir/backups"; sqlite3 "$$source" 'CREATE TABLE panel_users(id TEXT); INSERT INTO panel_users VALUES("fixture");'; ./deploy/panel-backup.sh "$$source" "$$backup_dir"; backup=$$(find "$$backup_dir" -type f -name 'admin-panel-*.db' -print -quit); checksum=$$(cat "$$backup.sha256"); printf '%s\n' '0000000000000000000000000000000000000000000000000000000000000000  tampered.db' > "$$backup.sha256"; if PANEL_RESTORE_CONFIRM=mcproxy-panel-restore ./deploy/panel-restore.sh "$$backup" "$$tampered" >/dev/null 2>&1; then echo 'checksum mismatch was accepted' >&2; exit 1; fi; printf '%s\n' "$$checksum" > "$$backup.sha256"; PANEL_RESTORE_CONFIRM=mcproxy-panel-restore ./deploy/panel-restore.sh "$$backup" "$$target"; test "$$(sqlite3 "$$target" 'SELECT id FROM panel_users;')" = fixture; mode=$$(stat -c '%a' "$$target" 2>/dev/null || stat -f '%Lp' "$$target"); test "$$mode" = 600; echo "Panel backup/restore checksum smoke test OK"

image-scan: docker-images
	./deploy/image-scan.sh mcproxy:local mcproxy-admin-panel:local

image-hardening-smoke: docker-images
	@test "$$(docker image inspect -f '{{.Config.User}}' mcproxy-admin-panel:local)" = node
	@docker image inspect -f '{{json .Config.Entrypoint}}' mcproxy:local | grep -F '"/app/mcproxy-init"' >/dev/null
	@docker image inspect -f '{{json .Config.Healthcheck.Test}}' mcproxy:local | grep -F 'healthcheck' >/dev/null
	@docker image inspect -f '{{json .Config.Healthcheck.Test}}' mcproxy-admin-panel:local | grep -F '/health' >/dev/null
	@echo 'Container image hardening smoke test OK'

security-audit:
	$(GOENV) go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	npm --prefix admin-panel audit --audit-level=high

supply-chain-check:
	./deploy/supply-chain-check.sh .

config-doc-check:
	./deploy/config-doc-check.sh .

config-doc-check-smoke:
	./deploy/config-doc-check-smoke.sh

preflight:
	MCPROXY_DB_DRIVER=postgres MCPROXY_POSTGRES_DSN=postgres://mcproxy:secret@db:5432/mcproxy MCPROXY_REDIS_ADDR=redis:6379 MCPROXY_REDIS_PASSWORD=preflight-redis-secret MCPROXY_REDIS_FAIL_CLOSED=true MCPROXY_ADMIN_TOKEN=preflight-admin-token-with-at-least-32-chars ./deploy/preflight.sh

preflight-smoke:
	sh -n deploy/preflight.sh deploy/preflight-smoke.sh
	./deploy/preflight-smoke.sh

panel-preflight:
	MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=https://panel.example.invalid MCPROXY_ADMIN_TOKEN=preflight-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=preflight-panel-session-secret-012345678901234567 PANEL_PREFLIGHT_REQUIRE_HTTPS=true ./deploy/panel-preflight.sh

panel-preflight-smoke:
	sh -n deploy/panel-preflight.sh
	./deploy/panel-preflight-smoke.sh
	@set -eu; tmpdir=$$(mktemp -d); trap 'chmod 755 "$$tmpdir/readonly" >/dev/null 2>&1 || true; rm -rf "$$tmpdir"' EXIT INT TERM; MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=https://panel.example.invalid MCPROXY_ADMIN_TOKEN=smoke-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=smoke-panel-session-secret-012345678901234567 ADMIN_PANEL_DB_PATH="$$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=true ./deploy/panel-preflight.sh; if MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=http://panel.example.invalid MCPROXY_ADMIN_TOKEN=smoke-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=smoke-panel-session-secret-012345678901234567 ADMIN_PANEL_DB_PATH="$$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=TRUE ./deploy/panel-preflight.sh >/dev/null 2>&1; then echo 'panel preflight accepted insecure ORIGIN with uppercase HTTPS requirement' >&2; exit 1; fi; if MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=https://panel.example.invalid MCPROXY_ADMIN_TOKEN=smoke-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=smoke-panel-session-secret-012345678901234567 ADMIN_PANEL_DB_PATH="$$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=maybe ./deploy/panel-preflight.sh >/dev/null 2>&1; then echo 'panel preflight accepted malformed HTTPS boolean' >&2; exit 1; fi; mkdir "$$tmpdir/readonly"; chmod 555 "$$tmpdir/readonly"; if MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=https://panel.example.invalid MCPROXY_ADMIN_TOKEN=smoke-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=smoke-panel-session-secret-012345678901234567 ADMIN_PANEL_DB_PATH="$$tmpdir/readonly/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=true ./deploy/panel-preflight.sh >/dev/null 2>&1; then echo 'panel preflight accepted a read-only database directory' >&2; exit 1; fi; echo 'Admin panel preflight smoke test OK'

kubernetes-production-check:
	./deploy/kubernetes/production-check.sh deploy/kubernetes/mcproxy.yaml deploy/kubernetes/admin-panel.yaml

kubernetes-production-check-smoke:
	sh -n deploy/kubernetes/production-check.sh deploy/kubernetes/production-check-smoke.sh
	./deploy/kubernetes/production-check-smoke.sh

kubernetes-rollout-rehearsal:
	./deploy/kubernetes/rollout-rehearsal.sh

firewall-check:
	@if command -v nft >/dev/null 2>&1; then nft -c -f deploy/firewall/mcproxy.nft.example; else echo "nft unavailable; skipped deploy/firewall/mcproxy.nft.example syntax check"; fi

promtool-check:
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" prom/prometheus:v3.8.0@sha256:d936808bdea528155c0154a922cd42fd75716b8bb7ba302641350f9f3eaeba09 check rules /etc/prometheus/alerts.yml
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability:/etc/prometheus:ro" prom/prometheus:v3.8.0@sha256:d936808bdea528155c0154a922cd42fd75716b8bb7ba302641350f9f3eaeba09 check config /etc/prometheus/prometheus.yml

run: ent tidy
	$(GOENV) go run $(PKG)

test:
	$(GOENV) go test -timeout=$(GO_TEST_TIMEOUT) ./...

vet:
	$(GOENV) go vet ./...

race:
	$(GOENV) go test -race -timeout=$(GO_TEST_TIMEOUT) ./...

check: test vet

panel-check:
	npm --prefix admin-panel run check-framework-versions
	npm --prefix admin-panel run check
	npm --prefix admin-panel run build

validate-deploy:
	for file in deploy/image-scan.sh deploy/supply-chain-check.sh deploy/config-doc-check.sh deploy/config-doc-check-smoke.sh deploy/preflight.sh deploy/preflight-smoke.sh deploy/panel-preflight.sh deploy/panel-preflight-smoke.sh deploy/observability/check-rules.sh deploy/kubernetes/rotate-secrets.sh deploy/kubernetes/production-check.sh deploy/kubernetes/production-check-smoke.sh deploy/kubernetes/rollout-rehearsal.sh deploy/backup.sh deploy/restore.sh deploy/panel-backup.sh deploy/panel-restore.sh deploy/postgres-restore-smoke.sh; do test -x "$$file"; done
	sh -n deploy/image-scan.sh deploy/preflight.sh deploy/preflight-smoke.sh deploy/panel-preflight.sh deploy/panel-preflight-smoke.sh deploy/observability/check-rules.sh deploy/kubernetes/rotate-secrets.sh deploy/kubernetes/production-check.sh deploy/kubernetes/production-check-smoke.sh deploy/kubernetes/rollout-rehearsal.sh
	bash -n deploy/backup.sh deploy/restore.sh deploy/panel-backup.sh deploy/panel-restore.sh deploy/postgres-restore-smoke.sh
	./deploy/observability/check-rules.sh deploy/observability/alerts.yml
	$(MAKE) supply-chain-check
	$(MAKE) config-doc-check
	$(MAKE) config-doc-check-smoke
	@if command -v nft >/dev/null 2>&1; then nft -c -f deploy/firewall/mcproxy.nft.example; else echo "nft unavailable; skipped firewall syntax check"; fi
	POSTGRES_PASSWORD="$${POSTGRES_PASSWORD:-ci-validation-only}" REDIS_PASSWORD="$${REDIS_PASSWORD:-ci-redis-validation-only}" docker compose -f deploy/docker-compose.yml config -q
	ruby -e 'require "yaml"; ARGV.each { |f| YAML.load_stream(File.read(f)) }' .github/workflows/ci.yml deploy/observability/prometheus.yml deploy/observability/alerts.yml deploy/docker-compose.smoke.yml deploy/kubernetes/mcproxy.yaml deploy/kubernetes/admin-panel.yaml deploy/kubernetes/network-policy.yaml deploy/kubernetes/external-secrets.example.yaml deploy/kubernetes/network-policy-egress.example.yaml deploy/kubernetes/ha/mcproxy.yaml
	@if command -v promtool >/dev/null 2>&1; then promtool check rules deploy/observability/alerts.yml && promtool check config deploy/observability/prometheus.yml; else echo "promtool unavailable; skipped Prometheus rule/config semantic check (run promtool before production rollout)"; fi

release-check:
	$(MAKE) check
	$(MAKE) race
	$(MAKE) panel-check
	$(MAKE) security-audit
	$(MAKE) supply-chain-check
	$(MAKE) config-doc-check
	$(MAKE) config-doc-check-smoke
	$(MAKE) validate-deploy
	$(MAKE) promtool-check
	$(MAKE) preflight-smoke
	$(MAKE) panel-preflight-smoke
	$(MAKE) kubernetes-production-check-smoke
	echo 'Release check OK (use gate-e2e, compose-smoke, and backup smoke targets for containerized rehearsal)'

docker-images:
	docker build --pull=false -t mcproxy:local --build-arg MCPROXY_VERSION="$(VERSION)" --build-arg MCPROXY_COMMIT="$(COMMIT)" --build-arg MCPROXY_BUILD_TIME="$(BUILD_TIME)" .
	docker build --pull=false -t mcproxy-admin-panel:local admin-panel

gate-e2e: docker-images
	test "$$(docker inspect -f '{{.Config.User}}' mcproxy:local)" = "nonroot:nonroot"
	docker rm -f mcproxy-gate-e2e >/dev/null 2>&1 || true
	docker run --rm -d --name mcproxy-gate-e2e -p 25565:25565 -e MCPROXY_ADMIN_TOKEN=e2e-token mcproxy:local >/dev/null
	trap 'docker stop mcproxy-gate-e2e >/dev/null 2>&1 || true' EXIT INT TERM; sleep 3; $(GOENV) go run ./cmd/mcproxy-statuscheck -addr 127.0.0.1:25565 -protocol 777

compose-smoke:
	@set -eu; project=mcproxy-compose-smoke-$$(date +%s)-$$$$; export COMPOSE_PROJECT_NAME=$$project POSTGRES_PASSWORD=ci-validation-only REDIS_PASSWORD=ci-redis-validation-only MCPROXY_REDIS_FAIL_CLOSED=true; compose='docker compose -p '$$project' -f deploy/docker-compose.yml -f deploy/docker-compose.smoke.yml'; container=$$project-mcproxy-1; trap "$$compose down -v" EXIT INT TERM; $$compose up -d --build; for attempt in $$(seq 1 30); do if $$compose exec -T mcproxy /app/mcproxy-init healthcheck; then break; fi; sleep 2; done; if ! $$compose exec -T mcproxy /app/mcproxy-init healthcheck; then $$compose ps; exit 1; fi; configured=$$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$$container" | awk -F= '$$1=="MCPROXY_REDIS_FAIL_CLOSED" {print $$2}'); test "$$configured" = true; $$compose stop redis >/dev/null; degraded=0; for attempt in $$(seq 1 15); do if ! $$compose exec -T mcproxy /app/mcproxy-init healthcheck >/dev/null 2>&1; then degraded=1; break; fi; sleep 1; done; test "$$degraded" = 1; echo "Compose smoke test OK ($$project; fail-closed=$$configured; redis-outage=503)"

panel-smoke:
	@set -eu; container=mcproxy-panel-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -e MCPROXY_API_URL=http://mcproxy.invalid -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://localhost:3000 -e PROTOCOL_HEADER=x-forwarded-proto -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password-1234 -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 -e ADMIN_PANEL_DB_PATH=/data/admin-panel.db mcproxy-admin-panel:local >/dev/null; for attempt in $$(seq 1 15); do if docker exec "$$container" node --input-type=module -e 'const base="http://127.0.0.1:3000", common={"X-Forwarded-Proto":"http"}; let r=await fetch(base+"/health",{headers:common}); if(r.status!==200) process.exit(1); r=await fetch(base+"/ready",{headers:common}); if(r.status!==200) process.exit(1); r=await fetch(base+"/login",{headers:common}); if(!r.ok||r.headers.get("x-frame-options")!=="DENY"||r.headers.get("x-content-type-options")!=="nosniff"||r.headers.get("cross-origin-opener-policy")!=="same-origin"||r.headers.get("cross-origin-resource-policy")!=="same-origin"||r.headers.get("x-permitted-cross-domain-policies")!=="none"||r.headers.get("content-security-policy")!=="default-src '\''self'\''; base-uri '\''none'\''; object-src '\''none'\''; frame-ancestors '\''none'\''; form-action '\''self'\''") process.exit(1); r=await fetch(base+"/login",{method:"POST",headers:{...common,Origin:base,"Content-Type":"application/x-www-form-urlencoded"},body:"username=admin&password=smoke-password-1234"}); if(r.status!==200) process.exit(1); const cookies=r.headers.getSetCookie?.()??[]; const cookie=(cookies[0]??r.headers.get("set-cookie")??"").split(";")[0]; if(!cookie) process.exit(1); r=await fetch(base+"/",{headers:{...common,Cookie:cookie}}); if(r.status!==200) process.exit(1); r=await fetch(base+"/members",{headers:{...common,Cookie:cookie}}); if(!r.ok) process.exit(1); r=await fetch(base+"/api/v1/stats",{headers:{...common,Cookie:cookie}}); if(r.status!==504) process.exit(1);'; then echo "Admin panel login/session smoke test OK"; exit 0; fi; sleep 1; done; docker logs "$$container"; exit 1

panel-dashboard-smoke:
	@set -eu; container=mcproxy-panel-dashboard-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -e MCPROXY_API_URL=http://mcproxy.invalid -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://localhost:3000 -e PROTOCOL_HEADER=x-forwarded-proto -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password-1234 -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 -e ADMIN_PANEL_DB_PATH=/data/admin-panel.db mcproxy-admin-panel:local >/dev/null; docker cp admin-panel/smoke-dashboard.mjs "$$container:/tmp/smoke-dashboard.mjs"; for attempt in $$(seq 1 15); do if docker exec "$$container" node /tmp/smoke-dashboard.mjs; then exit 0; fi; sleep 1; done; docker logs "$$container"; exit 1

panel-membership-smoke:
	@set -eu; container=mcproxy-panel-membership-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -e MCPROXY_API_URL=http://127.0.0.1:18080 -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://localhost:3000 -e PROTOCOL_HEADER=x-forwarded-proto -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password-1234 -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 -e ADMIN_PANEL_DB_PATH=/data/admin-panel.db mcproxy-admin-panel:local >/dev/null; docker cp admin-panel/smoke-membership.mjs "$$container:/tmp/smoke-membership.mjs"; for attempt in $$(seq 1 15); do if docker exec "$$container" node /tmp/smoke-membership.mjs; then exit 0; fi; sleep 1; done; docker logs "$$container"; exit 1

panel-migration-smoke:
	@set -eu; tmpdir=$$(mktemp -d); container=mcproxy-panel-migration-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true; rm -rf "$$tmpdir"' EXIT INT TERM; node admin-panel/scripts/create-legacy-db.mjs "$$tmpdir/admin-panel.db"; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -v "$$tmpdir:/data" -e MCPROXY_API_URL=http://mcproxy.invalid -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://127.0.0.1:3000 -e PROTOCOL_HEADER=x-forwarded-proto -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password-1234 -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 -e ADMIN_PANEL_DB_PATH=/data/admin-panel.db mcproxy-admin-panel:local >/dev/null; migrated=0; for attempt in $$(seq 1 15); do if docker exec "$$container" node --input-type=module -e 'const r=await fetch("http://127.0.0.1:3000/login",{method:"POST",headers:{Origin:"http://127.0.0.1:3000","X-Forwarded-Proto":"http","Content-Type":"application/x-www-form-urlencoded"},body:"username=admin&password=smoke-password-1234"}); if(![200,303].includes(r.status)) process.exit(1);'; then migrated=1; break; fi; sleep 1; done; test "$$migrated" = 1; test "$$(sqlite3 "$$tmpdir/admin-panel.db" "SELECT name FROM pragma_table_info('panel_users') WHERE name='must_change_password';")" = must_change_password; test "$$(sqlite3 "$$tmpdir/admin-panel.db" "SELECT version FROM panel_schema_meta WHERE component='membership';")" = 1; echo 'Admin panel legacy migration smoke test OK'

panel-security-smoke:
	@set -eu; container=mcproxy-panel-security-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -e MCPROXY_API_URL=http://mcproxy.invalid -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://localhost:3000 -e PROTOCOL_HEADER=x-forwarded-proto -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password-1234 -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 -e ADMIN_PANEL_DB_PATH=/data/admin-panel.db mcproxy-admin-panel:local >/dev/null; docker cp admin-panel/smoke-security.mjs "$$container:/tmp/smoke-security.mjs"; for attempt in $$(seq 1 15); do if docker exec "$$container" node /tmp/smoke-security.mjs; then exit 0; fi; sleep 1; done; docker logs "$$container"; exit 1

panel-rehearsal: panel-smoke panel-dashboard-smoke panel-membership-smoke panel-migration-smoke panel-security-smoke panel-backup-smoke
	@echo 'Admin panel rehearsal OK'

ops-rehearsal: release-check gate-e2e compose-smoke postgres-restore-smoke panel-rehearsal
	@echo 'Operations rehearsal OK (release, Gate, Compose, PostgreSQL restore, and admin panel)'

clean:
	rm -rf bin
	# Keep schema; remove generated code only
	rm -f internal/ent/*.go
