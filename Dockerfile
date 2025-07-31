# Build stage
FROM golang:1.21-alpine AS builder

# Install git and ca-certificates (needed for go mod download and git operations)
RUN apk add --no-cache git ca-certificates openssh-client

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kube-git-backup ./cmd

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS, git for git operations, and openssh for SSH
RUN apk --no-cache add ca-certificates git openssh-client && \
    mkdir -p /root/.ssh && \
    chmod 700 /root/.ssh && \
    ssh-keyscan github.com gitlab.com bitbucket.org >> /root/.ssh/known_hosts

# Create a non-root user
RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/kube-git-backup .

# Create necessary directories and set permissions
RUN mkdir -p /tmp/kube-backup /etc/ssh-key && \
    chown -R appuser:appgroup /app /tmp/kube-backup /etc/ssh-key

# Switch to non-root user
USER appuser

# Set the binary as executable
RUN chmod +x ./kube-git-backup

# Health check
HEALTHCHECK --interval=60s --timeout=10s --start-period=30s --retries=3 \
  CMD ps aux | grep '[k]ube-git-backup' || exit 1

# Run the application
CMD ["./kube-git-backup"]
