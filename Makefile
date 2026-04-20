.PHONY: all tools ent tidy build run test clean

BIN := bin/mcproxy
PKG := ./cmd/mcproxy

all: build

tools:
	GO111MODULE=on go install entgo.io/ent/cmd/ent@latest

# Generate ent code from schema
ent:
	@test -d internal/ent/schema || mkdir -p internal/ent/schema
	@if ls internal/ent/schema/*.go >/dev/null 2>&1; then \
		go run entgo.io/ent/cmd/ent generate --feature sql/upsert,privacy,lock,sql/schemaconfig ./internal/ent/schema; \
	else \
		echo "[ent] no schema .go files, skipping codegen"; \
	fi

tidy:
	go mod tidy

build: ent tidy
	go build -o $(BIN) $(PKG)

run: ent tidy
	go run $(PKG)

test:
	go test ./...

clean:
	rm -rf bin
	# Keep schema; remove generated code only
	rm -f internal/ent/*.go
