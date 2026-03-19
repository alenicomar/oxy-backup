# Multi-stage build for backup-lite
# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /backup-lite ./cmd/backup-lite/

# Stage 2: Minimal runtime with postgresql-client and git
FROM alpine:3.21

RUN apk add --no-cache \
    postgresql-client \
    git \
    ca-certificates \
    tzdata

COPY --from=builder /backup-lite /usr/local/bin/backup-lite

# Create a non-root user
RUN addgroup -S backuplite && adduser -S backuplite -G backuplite
USER backuplite

WORKDIR /data

ENTRYPOINT ["backup-lite"]
