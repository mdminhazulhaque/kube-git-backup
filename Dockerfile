# Build stage
FROM golang:1.24-bullseye AS builder

# Install git and ca-certificates (needed for go mod download and git operations)
RUN apt-get update && apt-get install -y git ca-certificates openssh-client && \
    rm -rf /var/lib/apt/lists/* && \
    update-ca-certificates

# Set working directory
WORKDIR /app

# Copy source code and vendor directory
COPY . .

# Build the application (using vendor directory)
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -mod=vendor -o kube-git-backup ./cmd

# Final stage
FROM ubuntu:22.04

# Install ca-certificates for HTTPS, git for git operations, and openssh for SSH
RUN apt-get update && \
    apt-get install -y ca-certificates git openssh-client && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /root/.ssh && \
    chmod 700 /root/.ssh && \
    (ssh-keyscan github.com >> /root/.ssh/known_hosts 2>/dev/null || true) && \
    (ssh-keyscan gitlab.com >> /root/.ssh/known_hosts 2>/dev/null || true) && \
    (ssh-keyscan bitbucket.org >> /root/.ssh/known_hosts 2>/dev/null || true)

# Create a non-root user
RUN groupadd -g 1000 appgroup && \
    useradd -u 1000 -g appgroup -s /bin/bash -m appuser

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
