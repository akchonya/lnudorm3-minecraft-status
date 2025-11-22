# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod ./
RUN go mod download

# Copy source code
COPY main.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server-checker .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

# Copy the binary from builder to /usr/local/bin (won't be overridden by volume mount)
COPY --from=builder /app/server-checker /usr/local/bin/server-checker
RUN chmod +x /usr/local/bin/server-checker

# Create directory for JSON file
RUN mkdir -p /data

# Set working directory to /data so JSON file is stored there
WORKDIR /data

# Run the application (binary is in PATH)
CMD ["server-checker"]

