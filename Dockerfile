FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ditto ./cmd/ditto


FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -S ditto && adduser -S -G ditto ditto

WORKDIR /app

COPY --from=builder /ditto /usr/local/bin/ditto
COPY config.yaml.example /app/config.yaml.example

# Data directory — mount a volume here in production
RUN mkdir -p /data/trash && chown -R ditto:ditto /data /app

USER ditto

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/ditto"]
CMD ["--config", "/app/config.yaml"]
