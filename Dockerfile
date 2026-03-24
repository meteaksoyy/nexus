# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o nexus ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN addgroup -S nexus && adduser -S nexus -G nexus

WORKDIR /app
COPY --from=builder /app/nexus .

USER nexus

EXPOSE 8080
ENTRYPOINT ["./nexus"]
