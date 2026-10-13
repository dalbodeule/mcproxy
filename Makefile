.PHONY: all tools ent tidy build migrate migration-smoke postgres-restore-smoke run test vet race check panel-check validate-deploy docker-images gate-e2e compose-smoke panel-smoke clean

BIN := bin/mcproxy
PKG := ./cmd/mcproxy
GOCACHE ?= /tmp/mcproxy-gocache
GOENV := GOCACHE=$(GOCACHE)

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
	$(GOENV) go build -o $(BIN) $(PKG)

migrate: build
	$(BIN) migrate

migration-smoke:
	docker build --pull=false -t mcproxy:migration-smoke .
	docker run --rm -e MCPROXY_DB_DRIVER=sqlite -e MCPROXY_SQLITE_PATH=/data/migration-smoke.db mcproxy:migration-smoke migrate

postgres-restore-smoke:
	./deploy/postgres-restore-smoke.sh

run: ent tidy
	$(GOENV) go run $(PKG)

test:
	$(GOENV) go test ./...

vet:
	$(GOENV) go vet ./...

race:
	$(GOENV) go test -race ./...

check: test vet

panel-check:
	npm --prefix admin-panel run check
	npm --prefix admin-panel run build

validate-deploy:
	POSTGRES_PASSWORD="$${POSTGRES_PASSWORD:-ci-validation-only}" REDIS_PASSWORD="$${REDIS_PASSWORD:-ci-redis-validation-only}" docker compose -f deploy/docker-compose.yml config -q
	ruby -e 'require "yaml"; ARGV.each { |f| YAML.load_stream(File.read(f)) }' deploy/observability/prometheus.yml deploy/observability/alerts.yml deploy/docker-compose.smoke.yml deploy/kubernetes/mcproxy.yaml deploy/kubernetes/admin-panel.yaml deploy/kubernetes/network-policy.yaml deploy/kubernetes/external-secrets.example.yaml

docker-images:
	docker build --pull=false -t mcproxy:local .
	docker build --pull=false -t mcproxy-admin-panel:local admin-panel

gate-e2e: docker-images
	docker rm -f mcproxy-gate-e2e >/dev/null 2>&1 || true
	docker run --rm -d --name mcproxy-gate-e2e -p 25565:25565 -e MCPROXY_ADMIN_TOKEN=e2e-token mcproxy:local >/dev/null
	trap 'docker stop mcproxy-gate-e2e >/dev/null 2>&1 || true' EXIT INT TERM; sleep 3; $(GOENV) go run ./cmd/mcproxy-statuscheck -addr 127.0.0.1:25565

compose-smoke:
	@set -eu; project=mcproxy-compose-smoke-$$(date +%s)-$$$$; export COMPOSE_PROJECT_NAME=$$project POSTGRES_PASSWORD=ci-validation-only REDIS_PASSWORD=ci-redis-validation-only; compose='docker compose -p '$$project' -f deploy/docker-compose.yml -f deploy/docker-compose.smoke.yml'; trap "$$compose down -v" EXIT INT TERM; $$compose up -d --build; for attempt in $$(seq 1 30); do if $$compose exec -T mcproxy /app/mcproxy-init healthcheck; then echo "Compose smoke test OK ($$project)"; exit 0; fi; sleep 2; done; $$compose ps; exit 1

panel-smoke:
	@set -eu; container=mcproxy-panel-smoke-$$(date +%s)-$$$$; trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; docker build --pull=false -t mcproxy-admin-panel:local admin-panel; docker run --rm -d --name "$$container" -e MCPROXY_API_URL=http://mcproxy.invalid -e MCPROXY_ADMIN_TOKEN=smoke-token -e ORIGIN=http://localhost:3000 -e ADMIN_PANEL_USERNAME=admin -e ADMIN_PANEL_PASSWORD=smoke-password -e ADMIN_PANEL_SESSION_SECRET=smoke-session-secret-012345678901234567890123456789 mcproxy-admin-panel:local >/dev/null; for attempt in $$(seq 1 15); do if docker exec "$$container" node -e 'fetch("http://127.0.0.1:3000/login").then(async r => { if (!r.ok || r.headers.get("x-frame-options") !== "DENY" || r.headers.get("x-content-type-options") !== "nosniff" || r.headers.get("cross-origin-opener-policy") !== "same-origin" || r.headers.get("cross-origin-resource-policy") !== "same-origin" || r.headers.get("x-permitted-cross-domain-policies") !== "none") process.exit(1); }).catch(() => process.exit(1))'; then echo "Admin panel smoke test OK"; exit 0; fi; sleep 1; done; docker logs "$$container"; exit 1

clean:
	rm -rf bin
	# Keep schema; remove generated code only
	rm -f internal/ent/*.go
