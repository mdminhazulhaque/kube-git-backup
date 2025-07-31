# Kube Git Backup - Makefile

.PHONY: build clean test fmt vet docker-build docker-push deploy undeploy help

# Variables
BINARY_NAME=kube-git-backup
DOCKER_IMAGE=ghcr.io/mdminhazulhaque/kube-git-backup
DOCKER_TAG=latest
NAMESPACE=kube-system

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o $(BINARY_NAME) ./cmd
	@echo "Built $(BINARY_NAME)"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	go clean
	rm -f $(BINARY_NAME)
	@echo "Cleaned"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	go vet ./...

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	golangci-lint run

# Run all checks
check: fmt vet test
	@echo "All checks passed"

# Build Docker image
docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo "Built Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)"

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	@echo "Pushed Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)"

# Generate Kubernetes manifests (for development)
generate-manifests:
	@echo "Generating Kubernetes manifests..."
	@echo "Manifests are already available in k8s/ directory"

# Deploy to Kubernetes
deploy:
	@echo "Deploying to Kubernetes namespace $(NAMESPACE)..."
	kubectl apply -f k8s/rbac.yaml
	kubectl apply -f k8s/deployment.yaml
	@echo "Deployed successfully"

# Undeploy from Kubernetes
undeploy:
	@echo "Removing from Kubernetes namespace $(NAMESPACE)..."
	kubectl delete -f k8s/deployment.yaml --ignore-not-found=true
	kubectl delete -f k8s/rbac.yaml --ignore-not-found=true
	@echo "Removed successfully"

# Show deployment status
status:
	@echo "Checking deployment status..."
	kubectl get pods -n $(NAMESPACE) -l app=kube-git-backup
	kubectl get deployment -n $(NAMESPACE) kube-git-backup

# View logs
logs:
	@echo "Showing logs..."
	kubectl logs -n $(NAMESPACE) -l app=kube-git-backup -f

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Development setup
dev-setup: deps
	@echo "Setting up development environment..."
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run locally (requires kubeconfig)
run-local:
	@echo "Running locally..."
	go run ./cmd

# Build and run locally
build-and-run: build
	@echo "Running built binary..."
	./$(BINARY_NAME)

# Create release build
release: clean check build
	@echo "Release build completed"

# Show help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run tests"
	@echo "  fmt           - Format code"
	@echo "  vet           - Vet code"
	@echo "  lint          - Lint code (requires golangci-lint)"
	@echo "  check         - Run fmt, vet, and test"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image"
	@echo "  deploy        - Deploy to Kubernetes"
	@echo "  undeploy      - Remove from Kubernetes"
	@echo "  status        - Show deployment status"
	@echo "  logs          - View application logs"
	@echo "  deps          - Install dependencies"
	@echo "  dev-setup     - Setup development environment"
	@echo "  run-local     - Run locally with go run"
	@echo "  build-and-run - Build and run the binary"
	@echo "  release       - Create release build"
	@echo "  help          - Show this help"

# Default target
.DEFAULT_GOAL := help
