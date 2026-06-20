# Build Stage
FROM golang:alpine AS builder

WORKDIR /src

# Download Go module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build UDR Mock Server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o udr cmd/udr/main.go

# Build MCP MongoDB Server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o mcp-mongo cmd/mcp-mongo/main.go

# Runner Stage
FROM alpine:latest

# Install ca-certificates (for HTTPS requests) and curl (for health checks)
RUN apk add --no-cache ca-certificates curl

WORKDIR /app

# Copy the pre-compiled binaries from the builder stage
COPY --from=builder /src/udr /app/udr
COPY --from=builder /src/mcp-mongo /app/mcp-mongo

# Expose ports:
# 8080: UDR Mock Server
# 8081: MCP Server (SSE)
EXPOSE 8080 8081

# By default, run the UDR mock server
CMD ["/app/udr"]
