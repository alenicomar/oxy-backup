# Multi-stage build for oxy-backup
# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /oxy ./cmd/oxy/

# Stage 2: Minimal runtime with postgresql-client and git
FROM alpine:3.21

RUN apk add --no-cache \
    postgresql-client \
    git \
    openssh-client \
    ca-certificates \
    tzdata

COPY --from=builder /oxy /usr/local/bin/oxy

# Create a non-root user
RUN addgroup -S oxy && adduser -S oxy -G oxy
USER oxy

WORKDIR /data

ENTRYPOINT ["oxy"]
