# syntax=docker/dockerfile:1.7

FROM golang:1.26-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/mcproxy ./cmd/mcproxy

FROM gcr.io/distroless/base-debian13:nonroot AS runner
WORKDIR /app

COPY --from=builder /out/mcproxy /app/mcproxy

EXPOSE 8080 25565

ENTRYPOINT ["/app/mcproxy"]

