# Multi-stage build for consolidation application
FROM golang:1.18-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o consolidation ./cmd/consolidation
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o consolidation-cli ./cmd/cli

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app/

COPY --from=builder /app/consolidation .
COPY --from=builder /app/consolidation-cli .

ENV GO_ENV=production
ENV ADDR=0.0.0.0
ENV PORT=3000

EXPOSE 3000

# Default command starts the web server
# For CLI mode, override CMD with the desired subcommand
CMD ["./consolidation"]
