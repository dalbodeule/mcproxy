# syntax=docker/dockerfile:1.7

FROM golang:1.27-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/mcproxy ./cmd/mcproxy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/mcproxy-init ./cmd/mcproxy-init
RUN mkdir -p /out/data && touch /out/data/.keep && chown -R 65532:65532 /out/data

FROM gcr.io/distroless/base-debian13@sha256:389cad21f73e4c37b94ffe5b13736d5a92bd5bd3c6c6b38c2be1c881e14ba2bd AS runner
WORKDIR /data

COPY --from=builder /out/mcproxy /app/mcproxy
COPY --from=builder /out/mcproxy-init /app/mcproxy-init
COPY --from=builder --chown=nonroot:nonroot /out/data/ /data/

ENV MCPROXY_HTTP_ADDR=0.0.0.0:8080
ENV MCPROXY_SQLITE_PATH=/data/mcproxy.db
VOLUME ["/data"]

EXPOSE 8080 25565

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 CMD ["/app/mcproxy-init", "healthcheck"]

ENTRYPOINT ["/app/mcproxy-init"]
